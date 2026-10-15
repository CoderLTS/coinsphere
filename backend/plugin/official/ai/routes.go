package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"coinsphere/backend/plugin/sdk"
	"github.com/gin-gonic/gin"
)

func (s *runtime) handleListAIModels(c *gin.Context, scope sdk.RouteScope) {
	data, err := s.ListAIModels(c.Request.Context())
	respond(c, data, err, "")
}

func (s *runtime) handleCreateAIModel(c *gin.Context, scope sdk.RouteScope) {
	payload, err := decodeBody[AIModelUpsertPayload](c)
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	data, err := s.CreateAIModel(c.Request.Context(), *payload, scope.(sdk.SystemScope))
	respond(c, data, err, "")
}

func (s *runtime) handleUpdateAIModel(c *gin.Context, scope sdk.RouteScope) {
	modelID, err := pathInt64(c, "modelId")
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	payload, err := decodeBody[AIModelUpsertPayload](c)
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	data, err := s.UpdateAIModel(c.Request.Context(), modelID, *payload, scope.(sdk.SystemScope))
	respond(c, data, err, "")
}

func (s *runtime) handlePatchAIModel(c *gin.Context, scope sdk.RouteScope) {
	modelID, err := pathInt64(c, "modelId")
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	payload, err := decodeBody[AIModelPatchPayload](c)
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	data, err := s.PatchAIModel(c.Request.Context(), modelID, *payload, scope.(sdk.SystemScope))
	respond(c, data, err, "")
}

func (s *runtime) handleDeleteAIModel(c *gin.Context, scope sdk.RouteScope) {
	modelID, err := pathInt64(c, "modelId")
	if err == nil {
		err = s.DeleteAIModel(c.Request.Context(), modelID)
	}
	respond(c, map[string]int64{"id": modelID}, err, "")
}

func (s *runtime) handleValidateAIModel(c *gin.Context, scope sdk.RouteScope) {
	modelID, err := pathInt64(c, "modelId")
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	data, err := s.ValidateAIModel(c.Request.Context(), modelID)
	respond(c, data, err, "")
}

func (s *runtime) handleListAssistantModels(c *gin.Context, scope sdk.RouteScope) {
	data, err := s.ListAssistantModels(c.Request.Context())
	respond(c, data, err, "")
}

func (s *runtime) handleListAssistantSessions(c *gin.Context, scope sdk.RouteScope) {
	data, err := s.ListAssistantSessions(c.Request.Context(), scope.(sdk.SystemScope))
	respond(c, data, err, "")
}

func (s *runtime) handleCreateAssistantSession(c *gin.Context, scope sdk.RouteScope) {
	payload, err := decodeBody[AssistantSessionCreatePayload](c)
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	data, err := s.CreateAssistantSession(c.Request.Context(), *payload, scope.(sdk.SystemScope))
	respond(c, data, err, "")
}

func (s *runtime) handleGetAssistantSession(c *gin.Context, scope sdk.RouteScope) {
	sessionID, err := pathInt64(c, "sessionId")
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	data, err := s.GetAssistantSession(c.Request.Context(), sessionID, scope.(sdk.SystemScope))
	respond(c, data, err, "")
}

func (s *runtime) handleDeleteAssistantSession(c *gin.Context, scope sdk.RouteScope) {
	sessionID, err := pathInt64(c, "sessionId")
	if err == nil {
		err = s.DeleteAssistantSession(c.Request.Context(), sessionID, scope.(sdk.SystemScope))
	}
	respond(c, map[string]int64{"id": sessionID}, err, "")
}

func (s *runtime) handleListAssistantMessages(c *gin.Context, scope sdk.RouteScope) {
	sessionID, err := pathInt64(c, "sessionId")
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	data, err := s.ListAssistantMessages(c.Request.Context(), sessionID, scope.(sdk.SystemScope))
	respond(c, data, err, "")
}

func (s *runtime) handleStreamAssistantSession(c *gin.Context, scope sdk.RouteScope) {
	sessionID, err := pathInt64(c, "sessionId")
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	payload, err := decodeBody[AssistantStreamPayload](c)
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()
	emit := func(event AssistantStreamEvent) error {
		raw, err := json.Marshal(event.Data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event.Name, raw); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}
	principal := scope.(sdk.SystemScope)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.host.Sessions.ValidateSession(ctx, nil) != nil {
					cancel()
					return
				}
			}
		}
	}()
	if err := s.StreamAssistantSession(ctx, sessionID, *payload, principal, emit); err != nil && c.Request.Context().Err() == nil {
		_ = emit(AssistantStreamEvent{Name: "error", Data: map[string]any{"code": 400, "msg": err.Error()}})
		_ = emit(AssistantStreamEvent{Name: "done", Data: map[string]any{}})
	}
}

func respond(c *gin.Context, data any, err error, _ string) {
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, ErrPermission):
			status = http.StatusUnauthorized
		case errors.Is(err, ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrConflict):
			status = http.StatusConflict
		}
		c.Header("Content-Type", "application/problem+json")
		c.JSON(status, map[string]any{
			"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": err.Error(),
			"requestId": c.Writer.Header().Get("X-Request-ID"),
		})
		return
	}
	c.JSON(http.StatusOK, map[string]any{"code": 200, "msg": "success", "data": data})
}
func decodeBody[T any](c *gin.Context) (*T, error) {
	var payload T
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("invalid AI request body")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("request must contain one object")
	}
	return &payload, nil
}
func pathInt64(c *gin.Context, name string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid path parameter")
	}
	return id, nil
}

func registerAIRoutes(registrar sdk.Registrar, runtime *runtime) error {
	for _, route := range []struct {
		method, path string
		handler      sdk.ScopedRouteHandler
	}{
		{"GET", "/models", runtime.handleListAIModels}, {"POST", "/models", runtime.handleCreateAIModel},
		{"PUT", "/models/:modelId", runtime.handleUpdateAIModel}, {"PATCH", "/models/:modelId", runtime.handlePatchAIModel},
		{"DELETE", "/models/:modelId", runtime.handleDeleteAIModel}, {"POST", "/models/:modelId/validations", runtime.handleValidateAIModel},
		{"GET", "/assistant/models", runtime.handleListAssistantModels}, {"GET", "/assistant/sessions", runtime.handleListAssistantSessions},
		{"POST", "/assistant/sessions", runtime.handleCreateAssistantSession}, {"GET", "/assistant/sessions/:sessionId", runtime.handleGetAssistantSession},
		{"DELETE", "/assistant/sessions/:sessionId", runtime.handleDeleteAssistantSession}, {"GET", "/assistant/sessions/:sessionId/messages", runtime.handleListAssistantMessages},
		{"POST", "/assistant/sessions/:sessionId/stream", runtime.handleStreamAssistantSession},
	} {
		handler := route.handler
		if err := registrar.Route(sdk.RouteDescriptor{Method: route.method, Pattern: route.path, Scope: sdk.ScopeSystem}, func(c *gin.Context, scope sdk.RouteScope) {
			if err := runtime.validateSession(c.Request.Context()); err != nil {
				respond(c, nil, err, "")
				return
			}
			handler(c, scope)
		}); err != nil {
			return err
		}
	}
	return nil
}
