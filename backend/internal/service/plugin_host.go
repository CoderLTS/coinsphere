package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"coinsphere/backend/internal/db"
	"coinsphere/backend/plugin/sdk"
	"gorm.io/gorm"
)

type platformQuery func(context.Context, json.RawMessage) (json.RawMessage, error)

var platformEmptyInputSchema = json.RawMessage(`{"type":"object","additionalProperties":false}`)

func (a *App) platformQueryCatalog() ([]sdk.AssistantQueryDescriptor, map[string]platformQuery) {
	definitions := []sdk.AssistantQueryDescriptor{}
	executions := map[string]platformQuery{}
	add := func(name, description string, schema json.RawMessage, execute platformQuery) {
		definitions = append(definitions, sdk.AssistantQueryDescriptor{Name: name, Description: description, InputSchema: schema})
		executions[name] = execute
	}

	add("platform_overview", "获取平台、数据库和主要业务对象数量的系统概览。", platformEmptyInputSchema,
		func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			data, err := a.platformOverview(ctx)
			return platformJSON(data, err)
		})
	add("list_plugins", "列出当前已编译加载的平台插件及贡献类型。", platformEmptyInputSchema,
		func(context.Context, json.RawMessage) (json.RawMessage, error) {
			items, err := a.ListInstalledPlugins()
			return platformJSON(map[string]any{"items": items}, err)
		})
	add("list_workflows", "按状态查询最近的平台工作流。", json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","enum":["inactive","active","error"]},"limit":{"type":"integer","minimum":1,"maximum":100,"default":20}},"additionalProperties":false}`),
		func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input struct {
				Status string `json:"status"`
				Limit  int    `json:"limit"`
			}
			if err := decodePlatformInput(raw, &input); err != nil {
				return nil, err
			}
			if input.Limit == 0 {
				input.Limit = 20
			}
			if input.Limit < 1 || input.Limit > 100 {
				return nil, errors.New("limit must be between 1 and 100")
			}
			items, err := a.ListWorkflows(ctx, input.Status)
			if len(items) > input.Limit {
				items = items[:input.Limit]
			}
			return platformJSON(map[string]any{"items": items}, err)
		})
	add("list_workflow_revisions", "查询指定工作流最近的修订摘要。", json.RawMessage(`{"type":"object","properties":{"workflowId":{"type":"integer","minimum":1},"limit":{"type":"integer","minimum":1,"maximum":50,"default":20}},"required":["workflowId"],"additionalProperties":false}`),
		func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input struct {
				WorkflowID int64 `json:"workflowId"`
				Limit      int   `json:"limit"`
			}
			if err := decodePlatformInput(raw, &input); err != nil {
				return nil, err
			}
			if input.WorkflowID <= 0 {
				return nil, errors.New("workflowId must be positive")
			}
			if input.Limit == 0 {
				input.Limit = 20
			}
			if input.Limit < 1 || input.Limit > 50 {
				return nil, errors.New("limit must be between 1 and 50")
			}
			if err := a.AuthorizeWorkflow(ctx, input.WorkflowID); err != nil {
				return nil, err
			}
			var rows []struct {
				ID                int64     `json:"id"`
				RevisionNumber    int64     `json:"revisionNumber"`
				MainTriggerNodeID string    `json:"mainTriggerNodeId"`
				CreatedAt         time.Time `json:"createdAt"`
			}
			err := a.DB.WithContext(ctx).Model(&db.WorkflowRevision{}).Where("workflow_id = ?", input.WorkflowID).
				Order("revision_number DESC").Limit(input.Limit).Find(&rows).Error
			return platformJSON(map[string]any{"items": rows}, err)
		})
	add("list_workflow_runs", "查询指定工作流最近的运行状态摘要。", json.RawMessage(`{"type":"object","properties":{"workflowId":{"type":"integer","minimum":1},"status":{"type":"string","maxLength":32},"limit":{"type":"integer","minimum":1,"maximum":50,"default":20}},"required":["workflowId"],"additionalProperties":false}`),
		func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input struct {
				WorkflowID int64  `json:"workflowId"`
				Status     string `json:"status"`
				Limit      int    `json:"limit"`
			}
			if err := decodePlatformInput(raw, &input); err != nil {
				return nil, err
			}
			if input.WorkflowID <= 0 {
				return nil, errors.New("workflowId must be positive")
			}
			if input.Limit == 0 {
				input.Limit = 20
			}
			if input.Limit < 1 || input.Limit > 50 {
				return nil, errors.New("limit must be between 1 and 50")
			}
			if err := a.AuthorizeWorkflow(ctx, input.WorkflowID); err != nil {
				return nil, err
			}
			query := a.DB.WithContext(ctx).Model(&db.WorkflowRun{}).Where("workflow_id = ?", input.WorkflowID)
			if input.Status != "" {
				query = query.Where("status = ?", input.Status)
			}
			var rows []struct {
				ID            int64     `json:"id"`
				RevisionID    int64     `json:"revisionId"`
				TriggerType   string    `json:"triggerType"`
				Status        string    `json:"status"`
				ErrorCategory *string   `json:"errorCategory"`
				TriggeredAt   time.Time `json:"triggeredAt"`
			}
			err := query.Order("id DESC").Limit(input.Limit).Find(&rows).Error
			return platformJSON(map[string]any{"items": rows}, err)
		})
	add("list_human_tasks", "查询最近的工作流人工任务。", json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","enum":["pending","approved","rejected","expired","superseded"]},"limit":{"type":"integer","minimum":1,"maximum":100,"default":20}},"additionalProperties":false}`),
		func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input struct {
				Status string `json:"status"`
				Limit  int    `json:"limit"`
			}
			if err := decodePlatformInput(raw, &input); err != nil {
				return nil, err
			}
			if input.Limit == 0 {
				input.Limit = 20
			}
			if input.Limit < 1 || input.Limit > 100 {
				return nil, errors.New("limit must be between 1 and 100")
			}
			items, err := a.ListWorkflowHumanTasks(ctx, input.Status)
			if len(items) > input.Limit {
				items = items[:input.Limit]
			}
			return platformJSON(map[string]any{"items": items}, err)
		})
	add("search_system_logs", "按级别、组件或关键词查询最近的结构化系统日志摘要。", json.RawMessage(`{"type":"object","properties":{"level":{"type":"string","enum":["debug","info","warn","error"]},"component":{"type":"string","maxLength":64},"keyword":{"type":"string","maxLength":100},"limit":{"type":"integer","minimum":1,"maximum":50,"default":20}},"additionalProperties":false}`),
		func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input struct {
				Level     string `json:"level"`
				Component string `json:"component"`
				Keyword   string `json:"keyword"`
				Limit     int    `json:"limit"`
			}
			if err := decodePlatformInput(raw, &input); err != nil {
				return nil, err
			}
			if input.Limit == 0 {
				input.Limit = 20
			}
			if input.Limit < 1 || input.Limit > 50 {
				return nil, errors.New("limit must be between 1 and 50")
			}
			query := a.DB.WithContext(ctx).Model(&db.SystemLog{})
			if input.Level != "" {
				query = query.Where("level = ?", input.Level)
			}
			if input.Component != "" {
				query = query.Where("component = ?", strings.TrimSpace(input.Component))
			}
			if input.Keyword != "" {
				query = query.Where("message ILIKE ?", "%"+strings.TrimSpace(input.Keyword)+"%")
			}
			var rows []struct {
				ID         int64     `json:"id"`
				LoggedAt   time.Time `json:"loggedAt"`
				Level      string    `json:"level"`
				Component  string    `json:"component"`
				Message    string    `json:"message"`
				Route      string    `json:"route"`
				StatusCode *int      `json:"statusCode"`
				DurationMS *int64    `json:"durationMs"`
			}
			err := query.Order("id DESC").Limit(input.Limit).Find(&rows).Error
			return platformJSON(map[string]any{"items": rows}, err)
		})
	add("notification_summary", "按渠道和状态汇总通知投递数量。", platformEmptyInputSchema,
		func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			var rows []struct {
				Channel string `json:"channel"`
				Status  string `json:"status"`
				Count   int64  `json:"count"`
			}
			err := a.DB.WithContext(ctx).Model(&db.NotificationDelivery{}).Where("recipient_user_id=?", ContextPrincipal(ctx).User.ID).
				Select("channel, status, COUNT(*) AS count").Group("channel, status").Order("channel, status").Scan(&rows).Error
			return platformJSON(map[string]any{"items": rows}, err)
		})
	add("list_node_capabilities", "查询当前实时工作流节点目录；指定 type 时返回完整 Schema。", json.RawMessage(`{"type":"object","properties":{"type":{"type":"string","maxLength":160}},"additionalProperties":false}`),
		func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input struct {
				Type string `json:"type"`
			}
			if err := decodePlatformInput(raw, &input); err != nil {
				return nil, err
			}
			definitions := a.ListWorkflowNodeDefinitions()
			if input.Type != "" {
				for _, definition := range definitions {
					if definition.Type == input.Type {
						return platformJSON(definition, nil)
					}
				}
				return nil, errors.New("workflow node type was not found")
			}
			items := make([]map[string]any, 0, len(definitions))
			for _, definition := range definitions {
				items = append(items, map[string]any{
					"type": definition.Type, "version": definition.Version, "title": definition.Title,
					"kind": definition.Kind, "available": definition.Available, "secretFields": definition.SecretFields,
				})
			}
			return platformJSON(map[string]any{"items": items}, nil)
		})
	return definitions, executions
}

func (a *App) platformOverview(ctx context.Context) (map[string]any, error) {
	overview, err := a.GetHomeOverview(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for name, model := range map[string]any{
		"users": &db.SystemUser{}, "workflows": &db.Workflow{}, "workflowRuns": &db.WorkflowRun{},
		"humanTasks": &db.WorkflowHumanTask{}, "notifications": &db.NotificationDelivery{},
	} {
		var count int64
		if err := a.DB.WithContext(ctx).Model(model).Count(&count).Error; err != nil {
			return nil, err
		}
		counts[name] = count
	}
	return map[string]any{"meta": a.GetHomeMeta(), "database": overview["database"], "counts": counts}, nil
}

func decodePlatformInput(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("tool input is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("tool input must contain one JSON object")
	}
	return nil
}

func platformJSON(value any, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, errors.New("tool query failed")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("encode tool result failed")
	}
	if len(raw) > 64<<10 {
		return nil, fmt.Errorf("tool result exceeds 64 KiB")
	}
	return raw, nil
}

func (a *App) ValidateSession(ctx context.Context, tx *gorm.DB) error {
	if tx == nil {
		tx = a.DB.WithContext(ctx)
	} else if err := lockAuthorization(tx); err != nil {
		return err
	}
	_, err := principalForTx(tx, ContextPrincipal(ctx))
	if errors.Is(err, ErrPermission) {
		return sdk.ErrSession
	}
	return err
}
func (a *App) QueryCatalog() []sdk.AssistantQueryDescriptor {
	catalog, _ := a.platformQueryCatalog()
	return catalog
}
func (a *App) QueryPlatform(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	if err := a.ValidateSession(ctx, nil); err != nil {
		return nil, err
	}
	catalog, queries := a.platformQueryCatalog()
	query, found := queries[name]
	if !found {
		return nil, ErrNotFound
	}
	var value any
	if json.Unmarshal(input, &value) != nil {
		return nil, errors.New("invalid platform query")
	}
	for _, desc := range catalog {
		if desc.Name == name && validateWorkflowSchemaValue(desc.InputSchema, value) != nil {
			return nil, errors.New("platform query does not match its schema")
		}
	}
	return query(ctx, input)
}
func (a *App) NodeCatalog() json.RawMessage {
	raw, _ := json.Marshal(a.ListWorkflowNodeDefinitions())
	return raw
}
func (a *App) RegisteredPlugins() []sdk.PluginDescriptor        { return a.Plugins.Plugins() }
func (a *App) AssistantQueries() []sdk.RegisteredAssistantQuery { return a.Plugins.AssistantQueries() }
func (a *App) RunAssistantQuery(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	if err := a.ValidateSession(ctx, nil); err != nil {
		return nil, err
	}
	for _, query := range a.Plugins.AssistantQueries() {
		if query.ToolName == name {
			scope, err := a.ResolveSystemScope(ctx, query.PluginID)
			if err != nil {
				return nil, err
			}
			return a.Plugins.RunAssistantQuery(ctx, name, input, scope)
		}
	}
	return nil, ErrNotFound
}
func (a *App) ValidateDraft(ctx context.Context, request sdk.DraftRequest) (sdk.DraftSummary, error) {
	if err := a.ValidateSession(ctx, nil); err != nil {
		return sdk.DraftSummary{}, err
	}
	name, description, raw := strings.TrimSpace(request.Name), strings.TrimSpace(request.Description), request.Graph
	if name == "" || utf8.RuneCountInString(name) > 120 || utf8.RuneCountInString(description) > 500 {
		return sdk.DraftSummary{}, errors.New("workflow name or description is invalid")
	}
	validated, err := a.validateWorkflowGraph(raw)
	if err != nil {
		return sdk.DraftSummary{}, err
	}
	var graph workflowGraph
	if json.Unmarshal([]byte(validated.graphJSON), &graph) != nil {
		return sdk.DraftSummary{}, errors.New("decode normalized workflow graph failed")
	}
	nodeTypes := make([]string, 0, len(validated.nodeTypes))
	seenTypes := map[string]bool{}
	for _, nodeType := range validated.nodeTypes {
		if !seenTypes[nodeType] {
			seenTypes[nodeType] = true
			nodeTypes = append(nodeTypes, nodeType)
		}
	}
	sort.Strings(nodeTypes)
	missingSecrets := make([]string, 0, len(validated.requiredSecrets))
	for key := range validated.requiredSecrets {
		missingSecrets = append(missingSecrets, key.nodeInstanceID+"."+key.field)
	}
	sort.Strings(missingSecrets)
	return sdk.DraftSummary{
		Name: name, Description: description, NodeCount: len(graph.Nodes), EdgeCount: len(graph.Edges),
		NodeTypes: nodeTypes, MissingSecrets: missingSecrets,
	}, nil
}

func (a *App) CreateDraft(ctx context.Context, request sdk.DraftRequest) (sdk.DraftSummary, error) {
	summary, err := a.ValidateDraft(ctx, request)
	if err != nil {
		return summary, err
	}
	workflow, err := a.CreateWorkflow(ctx, WorkflowCreatePayload{Name: summary.Name, Description: summary.Description, Graph: request.Graph}, ContextPrincipal(ctx))
	if err != nil {
		return sdk.DraftSummary{}, err
	}
	summary.WorkflowID, summary.Status = workflow.ID, WorkflowStatusInactive
	summary.EditURL = fmt.Sprintf("/scheduler/workflow/%d/edit", workflow.ID)
	return summary, nil
}
