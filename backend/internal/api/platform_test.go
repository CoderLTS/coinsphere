package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"coinsphere/backend/internal/config"
	"coinsphere/backend/internal/service"
	"coinsphere/backend/internal/testdb"
	"coinsphere/backend/plugin/official"
	"coinsphere/backend/plugin/sdk"
	"github.com/gorilla/websocket"
)

func TestHTTPResourceAuthorizationAndLongConnectionRevocation(t *testing.T) {
	_, database := testdb.Open(t, true)
	app := service.NewApp(database, &config.AppConfig{Auth: config.AuthConfig{SecretKey: "synthetic-api-key", PasswordIterations: 1, AccessTokenTTLMinutes: 60}}, sdk.NewRegistry())

	if err := database.Exec("INSERT INTO users(id,username,password_hash) VALUES(1,'coinsphere',?)", app.Hasher.HashPassword("synthetic-old")).Error; err != nil {
		t.Fatal(err)
	}
	ownerToken := app.Tokens.CreateAccessToken(1, 1, false).Value
	readerToken := ownerToken
	owner, err := app.AuthenticateAccessToken(ownerToken)
	if err != nil {
		t.Fatal(err)
	}
	ctx := service.WithPrincipal(context.Background(), owner)
	graph := json.RawMessage(`{"schemaVersion":3,"entryPoints":{"main":"manual"},"nodes":[{"nodeInstanceId":"manual","nodeType":"core.manual","nodeVersion":"1.0.0","config":{},"position":{"x":0,"y":0}},{"nodeInstanceId":"end","nodeType":"core.end","nodeVersion":"1.0.0","config":{},"position":{"x":100,"y":0}}],"edges":[{"edgeId":"end","sourceNodeInstanceId":"manual","sourcePort":"out","targetNodeInstanceId":"end","targetPort":"in"}]}`)
	w, err := app.CreateWorkflow(ctx, service.WorkflowCreatePayload{Name: "Synthetic", Graph: graph}, owner)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(app, t.TempDir(), t.TempDir(), t.TempDir())
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+readerToken)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	path := fmt.Sprintf("/api/v1/workflows/%d", w.ID)

	for _, token := range []string{"", app.Tokens.CreateAccessToken(1, 0, false).Value, app.Tokens.CreateAccessToken(2, 1, false).Value} {
		readerToken = token
		if response := request(http.MethodGet, path, ""); response.Code != http.StatusUnauthorized {
			t.Fatal("invalid session reached HTTP", response.Code)
		}
	}
	readerToken = ownerToken
	if response := request(http.MethodGet, path, ""); response.Code != http.StatusOK {
		t.Fatal("owner cannot read resource", response.Code)
	}
	for _, removed := range []string{"/api/v1/workbench", "/api/v1/admin/users", "/api/v1/admin/roles", "/api/v1/admin/permissions", "/api/v1/system/users", "/api/v1/system/roles", "/api/v1/system/capabilities", "/api/v1/system/menu-buttons", "/api/v1/result-views/1/grants"} {
		if response := request(http.MethodGet, removed, ""); response.Code != http.StatusNotFound {
			t.Fatal("removed API remains", removed, response.Code)
		}
	}
	if response := request(http.MethodPut, path+"/grants", `{}`); response.Code != http.StatusNotFound {
		t.Fatal("share API remains", response.Code)
	}
	if response := request(http.MethodGet, "/api/v1/me", ""); strings.Contains(response.Body.String(), "roleCodes") || strings.Contains(response.Body.String(), "permissions") {
		t.Fatal("me exposed user authorization")
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	connect := func(path, protocol string) *websocket.Conn {
		t.Helper()
		dialer := websocket.Dialer{Subprotocols: []string{protocol, readerToken}}
		connection, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+path, http.Header{"Origin": []string{server.URL}})
		if err != nil {
			t.Fatal("cannot establish authorized long connection", err)
		}
		return connection
	}
	workflowConn := connect(fmt.Sprintf("/api/v1/ws/workflows/%d/runs", w.ID), workflowRunsWSProtocol)
	defer workflowConn.Close()
	notificationConn := connect("/api/v1/ws/notifications", notificationWSProtocol)
	defer notificationConn.Close()
	closedWithinRevalidation := func(connection *websocket.Conn) {
		t.Helper()
		if err := connection.SetReadDeadline(time.Now().Add(8 * time.Second)); err != nil {
			t.Fatal(err)
		}
		for {
			_, _, err := connection.ReadMessage()
			if err == nil {
				continue // 撤销前已发送的通知快照可以仍在客户端接收缓冲中。
			}
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() {
				t.Fatal("long connection did not close after authorization changed", err)
			}
			return
		}
	}

	if response := request(http.MethodPut, "/api/v1/me/password", `{"currentPassword":"wrong","newPassword":"synthetic-new"}`); response.Code < 400 {
		t.Fatal("wrong password accepted")
	}
	if response := request(http.MethodPost, "/api/v1/auth/reauth", `{"password":"synthetic-old"}`); response.Code != http.StatusOK {
		t.Fatal("secondary confirmation failed", response.Code)
	}
	if response := request(http.MethodPut, "/api/v1/me/password", `{"currentPassword":"synthetic-old","newPassword":"synthetic-new"}`); response.Code != http.StatusOK {
		t.Fatal("password update failed", response.Code)
	}
	closedWithinRevalidation(workflowConn)
	closedWithinRevalidation(notificationConn)
	if response := request(http.MethodGet, path, ""); response.Code != http.StatusUnauthorized {
		t.Fatal("old session survived", response.Code)
	}
	session, err := app.Login("coinsphere", "synthetic-new", false)
	if err != nil {
		t.Fatal(err)
	}
	readerToken = session.AccessToken
	reader, err := app.AuthenticateAccessToken(readerToken)
	if err != nil {
		t.Fatal(err)
	}
	conn := connect("/api/v1/ws/notifications", notificationWSProtocol)
	defer conn.Close()
	if err := app.LogoutAccessToken(reader); err != nil {
		t.Fatal(err)
	}
	closedWithinRevalidation(conn)
	if response := request(http.MethodGet, path, ""); response.Code != http.StatusUnauthorized {
		t.Fatal("logout session survived", response.Code)
	}
	for _, token := range []string{"", ownerToken, app.Tokens.CreateAccessToken(2, 1, false).Value} {
		protocols := []string{notificationWSProtocol}
		if token != "" {
			protocols = append(protocols, token)
		}
		dialer := websocket.Dialer{Subprotocols: protocols}
		connection, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/ws/notifications", http.Header{"Origin": []string{server.URL}})
		if connection != nil {
			connection.Close()
			t.Fatal("invalid session reached WebSocket")
		}
		if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
			t.Fatal("invalid WebSocket rejection", err)
		}
	}
}

func TestDirectResultHTTPBoundary(t *testing.T) {
	_, database := testdb.Open(t, true)
	registry := sdk.NewRegistry()
	app := service.NewApp(database, &config.AppConfig{Auth: config.AuthConfig{SecretKey: "synthetic-results-key", PasswordIterations: 1, AccessTokenTTLMinutes: 60}}, registry)
	if err := database.Exec("INSERT INTO users(id,username) VALUES(1,'coinsphere')").Error; err != nil {
		t.Fatal(err)
	}
	if err := official.RegisterAll(registry, sdk.Host{Stores: sdk.GormPluginStores{Database: database}, Network: official.NetworkClientFactory{}}, map[string]bool{"official.binance": true, "official.quant": true}); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(app, t.TempDir(), t.TempDir(), t.TempDir())
	token := app.Tokens.CreateAccessToken(1, 1, false).Value
	for _, tc := range []struct {
		path, token string
		status      int
	}{
		{"/api/v1/plugins/official.binance/results/paper", token, 200},
		{"/api/v1/plugins/official.quant/results/backtests", token, 200},
		{"/api/v1/plugins/official.binance/results/paper?workflowId=99999", token, 404},
		{"/api/v1/plugins/official.binance/results/paper?userId=1", token, 400},
		{"/api/v1/plugins/official.quant/results/backtests?workflowId=bad", token, 400},
		{"/api/v1/plugins/official.binance/results/paper", "", 401},
		{"/api/v1/plugins/official.quant/results/backtests", app.Tokens.CreateAccessToken(1, 0, false).Value, 401},
		{"/api/v1/plugins/official.binance/results/paper", app.Tokens.CreateAccessToken(2, 1, false).Value, 401},
		{"/api/v1/plugins/official.binance/results/unknown", token, 404},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != tc.status {
			t.Fatal(tc.path, recorder.Code, recorder.Body.String())
		}
	}
	if err := database.Exec("UPDATE plugin_installations SET status='uninstalled' WHERE plugin_id='official.binance'").Error; err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/official.binance/results/paper", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != 404 {
		t.Fatal("uninstalled plugin results remained accessible", recorder.Code)
	}
}
