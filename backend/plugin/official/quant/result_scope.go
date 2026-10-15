package quant

import (
	"coinsphere/backend/plugin/sdk"
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
)

var quantResultScopeSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"workflowId":{"type":"integer","minimum":1,"title":"工作流编号"},"nodeInstanceId":{"type":"string","minLength":1,"maxLength":128,"title":"节点标识"}},"dependentRequired":{"nodeInstanceId":["workflowId"]},"additionalProperties":false}`)
var quantResultFilterSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"venue":{"type":"string","title":"交易所","pattern":"^[a-z][a-z0-9_-]{1,31}$"},"market":{"type":"string","title":"市场","enum":["spot","usdm"],"enumLabels":["现货","U 本位合约"]},"instrument":{"type":"string","title":"交易对","pattern":"^[A-Z0-9]{2,32}$"},"limit":{"type":"integer","title":"条数","minimum":1,"maximum":200}},"additionalProperties":false}`)

type quantResultScope struct {
	WorkflowID     int64  `json:"workflowId"`
	NodeInstanceID string `json:"nodeInstanceId"`
}

func quantResultResources(raw json.RawMessage) ([]sdk.WorkflowResource, error) {
	var scope quantResultScope
	if json.Unmarshal(raw, &scope) != nil || scope.WorkflowID < 0 || scope.NodeInstanceID != "" && scope.WorkflowID == 0 {
		return nil, errors.New("量化结果范围无效")
	}
	if scope.WorkflowID == 0 {
		return []sdk.WorkflowResource{}, nil
	}
	return []sdk.WorkflowResource{{WorkflowID: scope.WorkflowID, NodeInstanceID: scope.NodeInstanceID}}, nil
}
func validateQuantResultScope(ctx context.Context, tx *gorm.DB, raw json.RawMessage) error {
	var scope quantResultScope
	if json.Unmarshal(raw, &scope) != nil {
		return errors.New("量化查询范围无效")
	}
	if scope.WorkflowID == 0 {
		return nil
	}
	var rows []struct{ GraphJSON string }
	if err := tx.WithContext(ctx).Table("workflow_revisions").Select("graph_json").Where("workflow_id=?", scope.WorkflowID).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var g struct {
			Nodes []struct {
				ID   string `json:"nodeInstanceId"`
				Type string `json:"nodeType"`
			}
		}
		if json.Unmarshal([]byte(row.GraphJSON), &g) != nil {
			return errors.New("量化工作流定义无效")
		}
		for _, node := range g.Nodes {
			if (scope.NodeInstanceID == "" || scope.NodeInstanceID == node.ID) && (node.Type == "official.quant.backtest" || node.Type == "official.quant.backtest_start") {
				return nil
			}
		}
	}
	return errors.New("查询范围须引用量化回测节点")
}
func validQuantBacktestScope(scope sdk.RouteScope) bool {
	if validQuantSystemScope(scope) {
		return true
	}
	result, ok := scope.(sdk.ResultScope)
	return ok && result.PluginID == quantPluginID && result.PageKey == "backtests" && result.UserID > 0
}
func quantBacktestQuery(query *gorm.DB, scope sdk.RouteScope) *gorm.DB {
	result, ok := scope.(sdk.ResultScope)
	if !ok {
		return sdk.ScopeWorkflows(query, scope, "workflow_id")
	}
	var fixed quantResultScope
	if json.Unmarshal(result.Scope, &fixed) != nil {
		return query.Where("FALSE")
	}
	if fixed.WorkflowID > 0 {
		query = query.Where("workflow_id=?", fixed.WorkflowID)
	}
	if fixed.NodeInstanceID != "" {
		query = query.Where("node_instance_id=?", fixed.NodeInstanceID)
	}
	var filters map[string]any
	if json.Unmarshal(result.Filters, &filters) != nil {
		return query.Where("FALSE")
	}
	for _, key := range []string{"venue", "market", "instrument"} {
		if value, ok := filters[key].(string); ok && value != "" {
			query = query.Where(key+"=?", value)
		}
	}
	return query
}
