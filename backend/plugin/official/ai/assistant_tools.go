package ai

import (
	"bytes"
	"coinsphere/backend/plugin/sdk"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func (a *runtime) assistantToolCatalog(principal sdk.SystemScope) ([]assistantToolDefinition, map[string]assistantToolExecution) {
	definitions := []assistantToolDefinition{}
	executions := map[string]assistantToolExecution{}
	add := func(name, description string, schema json.RawMessage, execute assistantToolExecution) {
		definitions = append(definitions, assistantToolDefinition{Type: "function", Function: assistantToolDefinitionFunction{Name: name, Description: description, Parameters: schema}})
		executions[name] = func(ctx context.Context, input json.RawMessage) (json.RawMessage, *assistantWorkflowCreateSummary, error) {
			if err := a.host.Sessions.ValidateSession(ctx, nil); err != nil {
				return nil, nil, err
			}
			return execute(ctx, input)
		}
	}
	for _, item := range a.host.Platform.QueryCatalog() {
		query := item
		add(query.Name, query.Description, query.InputSchema, func(ctx context.Context, input json.RawMessage) (json.RawMessage, *assistantWorkflowCreateSummary, error) {
			result, err := a.host.Platform.QueryPlatform(ctx, query.Name, input)
			return result, nil, err
		})
	}
	for _, item := range a.host.Platform.AssistantQueries() {
		query := item
		add(query.ToolName, query.Descriptor.Description, query.Descriptor.InputSchema, func(ctx context.Context, input json.RawMessage) (json.RawMessage, *assistantWorkflowCreateSummary, error) {
			result, err := a.host.Platform.RunAssistantQuery(ctx, query.ToolName, input)
			return result, nil, err
		})
	}
	var created *assistantWorkflowCreateSummary
	add("create_workflow", "校验并创建停用草稿；不会发布、激活或运行工作流。", json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":120},"description":{"type":"string","maxLength":500},"graph":{"type":"object"}},"required":["name","description","graph"],"additionalProperties":false}`), func(ctx context.Context, input json.RawMessage) (json.RawMessage, *assistantWorkflowCreateSummary, error) {
		if created != nil {
			raw, _, err := assistantToolJSON(created, nil)
			return raw, created, err
		}
		var request sdk.DraftRequest
		if err := decodeAssistantToolInput(input, &request); err != nil {
			return nil, nil, err
		}
		summary, err := a.createAssistantWorkflow(ctx, request.Name, request.Description, request.Graph, principal)
		if err != nil {
			return nil, nil, err
		}
		created = summary
		raw, _, err := assistantToolJSON(summary, nil)
		return raw, summary, err
	})
	return definitions, executions
}

func (a *runtime) assistantSystemPrompt(tools []assistantToolDefinition) string {
	toolDirectory := make([]map[string]string, 0, len(tools))
	for _, tool := range tools {
		toolDirectory = append(toolDirectory, map[string]string{"name": tool.Function.Name, "description": tool.Function.Description})
	}
	contextData, _ := json.Marshal(map[string]any{
		"plugins": a.host.Platform.RegisteredPlugins(), "nodes": a.host.Platform.NodeCatalog(), "tools": toolDirectory,
	})
	return `你是 CoinSphere 平台内置智能助手，为个人账号服务。你的职责是解释平台、通过工具查询平台数据，并根据用户描述生成工作流。

平台知识：CoinSphere 是基于 Go、PostgreSQL 16 和 Vue 的模块化单体工作流平台。工作流负责粗粒度编排，状态默认为 inactive；只有用户明确操作才可激活或运行。时间统一使用 UTC，金融数值使用十进制字符串。插件在编译期通过 SDK 注册节点、页面、路由和只读助手查询。AI、工作流和通用 HTTP 节点不得调用交易所私有接口或绕过风控。

回答规则：平台事实优先使用下面的实时目录和只读工具；不知道时明确说明，不编造数据。工具参数和结果不得复述为原始载荷，也不要展示个人数据。不要输出思维链。

工作流规则：仅当用户明确要求创建工作流时才创建。必须使用实时节点版本、端口和 JSON Schema。图必须是 schemaVersion=3，entryPoints.main 指向主触发器；输入使用 input，节点输出使用 nodes["节点ID"]，包含且仅包含一个主触发器；每个节点必须有 nodeInstanceId、nodeType、nodeVersion、config、position；每条边必须有 edgeId、sourceNodeInstanceId、sourcePort、targetNodeInstanceId、targetPort。密钥字段绝不能写入 graph.config。完成图后必须调用 create_workflow；该工具会先由平台校验，校验失败时根据错误修正后重试。工具成功即表示工作流已经创建，必须向用户说明 workflowId、inactive 状态、编辑地址和待补密钥，不要要求确认、不要生成方案卡、不要重复创建，也绝不自动激活或运行。

实时平台目录：` + string(contextData)
}

func decodeAssistantToolInput(raw json.RawMessage, target any) error {
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

func assistantToolJSON(value any, err error) (json.RawMessage, *assistantWorkflowCreateSummary, error) {
	if err != nil {
		return nil, nil, errors.New("tool query failed")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, errors.New("encode tool result failed")
	}
	if len(raw) > 64<<10 {
		return nil, nil, fmt.Errorf("tool result exceeds 64 KiB")
	}
	return raw, nil, nil
}
