// Package business is a small, non-financial SDK 5 extension.
package business

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"coinsphere/backend/plugin/sdk"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const read = "plugins.example.business.read"
const execute = "plugins.example.business.execute"

var empty = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false}`)

func resources(raw json.RawMessage) ([]sdk.WorkflowResource, error) {
	var value struct {
		WorkflowID int64 `json:"workflowId"`
	}
	if json.Unmarshal(raw, &value) != nil || value.WorkflowID < 0 {
		return nil, errors.New("invalid business workflow scope")
	}
	if value.WorkflowID == 0 {
		return []sdk.WorkflowResource{}, nil
	}
	return []sdk.WorkflowResource{{WorkflowID: value.WorkflowID}}, nil
}

func Register(r sdk.Registrar, host sdk.Host) error {
	if err := r.Action(sdk.NodeDescriptor{
		Type: "example.business.task", Version: "1.0.0", Kind: sdk.NodeKindAction,
		Title: "业务事项", Description: "生成一个可在插件菜单确认的业务事项", Category: "business",
		ConfigSchema: empty, InputSchema: empty, UISchema: json.RawMessage(`{}`),
		OutputSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"message":{"type":"string"}},"required":["message"],"additionalProperties":false}`),
		Pool:         sdk.PoolCompute, State: sdk.StateStateless, SideEffect: sdk.SideEffectNone,
	}, task{}); err != nil {
		return err
	}
	if err := r.RunPanel(sdk.RunPanelDescriptor{PanelKey: "task", Title: "业务事项", NodeTypes: []string{"example.business.task"}, ComponentEntry: "TaskPanel.vue"}); err != nil {
		return err
	}
	if err := r.ResultPage(sdk.ResultPageDescriptor{
		PageKey: "tasks", Title: "业务事项", ComponentEntry: "TaskResults.vue",
		ScopeSchema:  json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"workflowId":{"type":"integer","minimum":1}},"additionalProperties":false}`),
		FilterSchema: empty, Actions: []string{"ack"}, Resources: resources,
		ValidateScope: func(ctx context.Context, tx *gorm.DB, raw json.RawMessage) error {
			refs, err := resources(raw)
			if err != nil {
				return err
			}
			if len(refs) == 0 {
				return nil
			}
			var count int64
			if err := tx.WithContext(ctx).Table("workflows").Where("id=?", refs[0].WorkflowID).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return errors.New("business workflow does not exist")
			}
			return nil
		},
	}); err != nil {
		return err
	}
	if err := r.Route(sdk.RouteDescriptor{Method: http.MethodGet, Pattern: "/summary", Scope: sdk.ScopeWorkflow}, func(c *gin.Context, scope sdk.RouteScope) {
		workflow := scope.(sdk.WorkflowScope)
		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "", "data": gin.H{"workflowId": workflow.WorkflowID, "message": "业务事项可读取"}})
	}); err != nil {
		return err
	}
	if err := r.Route(sdk.RouteDescriptor{Method: http.MethodGet, Pattern: "/tasks", Scope: sdk.ScopeResult, ResultPageKey: "tasks"}, func(c *gin.Context, scope sdk.RouteScope) {
		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "", "data": gin.H{"message": "业务事项可读取"}})
	}); err != nil {
		return err
	}
	// 业务动作在同一事务内复验会话与范围。
	return r.Route(sdk.RouteDescriptor{Method: http.MethodPost, Pattern: "/ack", Scope: sdk.ScopeResult, ResultPageKey: "tasks", Action: "ack"}, func(c *gin.Context, scope sdk.RouteScope) {
		result := scope.(sdk.ResultScope)
		if host.Store == nil || result.Revalidate == nil {
			c.JSON(http.StatusForbidden, gin.H{"type": "about:blank", "title": "Forbidden", "status": 403, "detail": "missing result write scope"})
			return
		}
		err := host.Store.DB().WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			return result.Revalidate(c.Request.Context(), tx)
		})
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"type": "about:blank", "title": "Forbidden", "status": 403, "detail": "result action scope changed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "", "data": gin.H{"message": "事项已确认"}})
	})
}

type task struct{}

func (task) Execute(context.Context, sdk.ActionRequest) (sdk.ActionResult, error) {
	return sdk.ActionResult{Output: json.RawMessage(`{"message":"业务事项可读取"}`)}, nil
}
