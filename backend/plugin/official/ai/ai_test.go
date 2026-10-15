package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"coinsphere/backend/internal/config"
	"coinsphere/backend/internal/db"
	"coinsphere/backend/internal/service"
	"coinsphere/backend/internal/testdb"
	"coinsphere/backend/plugin/sdk"
	"github.com/gin-gonic/gin"
)

func aiFixture(t *testing.T) (*runtime, *service.App, context.Context, *service.Principal, sdk.SystemScope) {
	t.Helper()
	_, database := testdb.Open(t, true)
	user := db.SystemUser{Username: "coinsphere", IsActive: true}
	if err := database.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	app := service.NewApp(database, &config.AppConfig{Auth: config.AuthConfig{SecretKey: "synthetic-ai-key", PasswordIterations: 1}}, sdk.NewRegistry())
	p := &service.Principal{User: &user, AccessTokenID: "synthetic-session", AccessTokenExp: time.Now().UTC().Add(time.Hour)}
	ctx := service.WithPrincipal(context.Background(), p)
	return &runtime{DB: database, host: sdk.Host{Platform: app, Sessions: app, Credentials: app.Cipher}}, app, ctx, p, sdk.SystemScope{UserID: user.ID, PluginID: pluginID, AllWorkflows: true}
}

func TestModelNodeReadsConfigurationOncePerCall(t *testing.T) {
	a, _, ctx, _, scope := aiFixture(t)
	started, release := make(chan string, 1), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer synthetic-provider-key" || r.Header.Get("Idempotency-Key") == "" {
			t.Error("model request lost endpoint, credential or operation key")
		}
		if calls.Add(1) == 1 {
			started <- body.Model
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": body.Model, "choices": []any{map[string]any{"message": map[string]any{"content": `{"answer":"ok"}`}}}, "usage": map[string]any{"total_tokens": 3}})
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	key := "synthetic-provider-key"
	payload := AIModelUpsertPayload{DisplayName: "本地模型", BaseURL: server.URL + "/v1", ModelName: "first", APIKey: &key}
	model, err := a.CreateAIModel(ctx, payload, scope)
	if err != nil {
		t.Fatal(err)
	}
	if model.APIKeyMasked != "********" {
		t.Fatal("model key was not masked")
	}
	var stored AIModelConfig
	if err := a.DB.First(&stored, model.ID).Error; err != nil || stored.APIKeyCiphertext == key {
		t.Fatal("key was not encrypted", err)
	}
	request := sdk.ActionRequest{Config: mustMarshal(map[string]any{"modelId": model.ID}), Input: json.RawMessage(`{"prompt":"test","data":{"value":"1"}}`), OperationKey: strings.Repeat("a", 64)}
	action := aiModelCallAction{runtime: a}
	type outcome struct {
		result sdk.ActionResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { result, err := action.Execute(ctx, request); done <- outcome{result, err} }()
	select {
	case name := <-started:
		if name != "first" {
			t.Fatal(name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("model request did not start")
	}
	payload.ModelName = "second"
	payload.APIKey = nil
	if _, err := a.UpdateAIModel(ctx, model.ID, payload, scope); err != nil {
		t.Fatal(err)
	}
	close(release)
	first := <-done
	if first.err != nil || !strings.Contains(string(first.result.Output), `"model":"first"`) {
		t.Fatal("in-flight config changed", first.err)
	}
	next, err := action.Execute(ctx, request)
	if err != nil || !strings.Contains(string(next.Output), `"model":"second"`) {
		t.Fatal("updated config was not used", err)
	}
	disabled := false
	if _, err := a.PatchAIModel(ctx, model.ID, AIModelPatchPayload{IsEnabled: &disabled}, scope); err != nil {
		t.Fatal(err)
	}
	if _, err := action.Execute(ctx, request); err == nil || !strings.Contains(err.Error(), "已停用") {
		t.Fatal("disabled model accepted", err)
	}
	request.Config = json.RawMessage(`{"modelId":999999}`)
	if _, err := action.Execute(ctx, request); !errors.Is(err, sdk.ErrNotFound) {
		t.Fatal("missing model accepted", err)
	}
	if calls.Load() != 2 {
		t.Fatal("unavailable models made external calls")
	}
}

func TestAssistantStreamDraftAndSessionRevocation(t *testing.T) {
	a, app, ctx, p, scope := aiFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	model, err := a.CreateAIModel(ctx, AIModelUpsertPayload{DisplayName: "Synthetic", BaseURL: server.URL, ModelName: "synthetic"}, scope)
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateAssistantSession(ctx, AssistantSessionCreatePayload{ModelID: model.ID}, scope)
	if err != nil {
		t.Fatal(err)
	}
	events := []string{}
	if err := a.StreamAssistantSession(ctx, session.ID, AssistantStreamPayload{Text: "hello"}, scope, func(e AssistantStreamEvent) error { events = append(events, e.Name); return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "user,content,done" {
		t.Fatal(events)
	}
	graph := json.RawMessage(`{"schemaVersion":3,"entryPoints":{"main":"start"},"nodes":[{"nodeInstanceId":"start","nodeType":"core.manual","nodeVersion":"1.0.0","config":{},"position":{"x":0,"y":0}},{"nodeInstanceId":"end","nodeType":"core.end","nodeVersion":"1.0.0","config":{},"position":{"x":260,"y":0}}],"edges":[{"edgeId":"end","sourceNodeInstanceId":"start","sourcePort":"out","targetNodeInstanceId":"end","targetPort":"in"}]}`)
	draft, err := a.createAssistantWorkflow(ctx, "Synthetic", "", graph, scope)
	if err != nil {
		t.Fatal(err)
	}
	var workflow db.Workflow
	if err := a.DB.First(&workflow, draft.WorkflowID).Error; err != nil || draft.Status != "inactive" || workflow.Status != "inactive" || workflow.PublishedRevisionID != nil {
		t.Fatal("assistant published or activated its draft", err)
	}
	err = a.StreamAssistantSession(ctx, session.ID, AssistantStreamPayload{Text: "invalidate"}, scope, func(e AssistantStreamEvent) error {
		if e.Name == "content" {
			return app.LogoutAccessToken(p)
		}
		return nil
	})
	if !errors.Is(err, sdk.ErrSession) {
		t.Fatal("stream committed after logout", err)
	}
	messages, err := a.ListAssistantMessages(context.Background(), session.ID, scope)
	if err != nil || len(messages) != 3 {
		t.Fatal("revoked stream stored a response", err)
	}
	if _, err := a.CreateAIModel(ctx, AIModelUpsertPayload{DisplayName: "Blocked", BaseURL: server.URL, ModelName: "synthetic"}, scope); !errors.Is(err, sdk.ErrSession) {
		t.Fatal("revoked session wrote a model", err)
	}
	if _, err := a.createAssistantWorkflow(ctx, "Blocked", "", graph, scope); !errors.Is(err, sdk.ErrSession) {
		t.Fatal("revoked session wrote a draft", err)
	}
}

func TestModelEndpointAndResponseBoundaries(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1:8080/v1", "http://192.168.1.10/v1", "https://models.example.test/v1"} {
		if _, err := normalizeAIBaseURL(value); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{"ftp://models.example.test", "http://user:pass@localhost/v1", "http://localhost/v1?key=synthetic", "https://api.binance.com/v1"} {
		if _, err := normalizeAIBaseURL(value); err == nil {
			t.Fatal("invalid model endpoint accepted")
		}
	}
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Store(true) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	model := assistantModelRuntime{BaseURL: redirect.URL}
	request, _ := http.NewRequest(http.MethodGet, model.BaseURL, nil)
	response, err := model.client(time.Second).Do(request)
	if response != nil {
		response.Body.Close()
	}
	if err == nil || redirected.Load() {
		t.Fatal("model redirect followed")
	}
	if _, err := readBounded(strings.NewReader("12345"), 4); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestAIRouteErrorsUseProblemDetails(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"invalid", errors.New("invalid model"), http.StatusBadRequest},
		{"session", fmt.Errorf("revoked: %w", sdk.ErrSession), http.StatusUnauthorized},
		{"missing", sdk.ErrNotFound, http.StatusNotFound},
		{"conflict", sdk.ErrConflict, http.StatusConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Header("X-Request-ID", "synthetic-request")
			respond(ctx, nil, test.err, "")
			var problem struct {
				Type, Title, Detail, RequestID string
				Status                         int
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.status || recorder.Header().Get("Content-Type") != "application/problem+json" ||
				problem.Type != "about:blank" || problem.Title != http.StatusText(test.status) || problem.Status != test.status ||
				problem.Detail != test.err.Error() || problem.RequestID != "synthetic-request" {
				t.Fatal("AI route violated the HTTP error contract", recorder.Code, recorder.Header(), recorder.Body.String())
			}
		})
	}
}
