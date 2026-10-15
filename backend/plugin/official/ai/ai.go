package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"coinsphere/backend/plugin/sdk"
)

const (
	pluginID           = "official.ai"
	maxAIResponseBytes = 1 << 20
)

var emptyObjectSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false}`)

func Register(registrar sdk.Registrar, host sdk.Host) error {
	if host.Store == nil || host.Credentials == nil || host.Platform == nil || host.Sessions == nil {
		return errors.New("AI 宿主服务不可用")
	}
	runtime := &runtime{DB: host.Store.DB(), host: host}
	if err := registrar.Action(sdk.NodeDescriptor{
		EditorKey: "model", Type: "official.ai.model_call", Version: "1.0.0", Kind: sdk.NodeKindAction,
		Title: "AI 模型调用", Description: "调用已配置的 AI 模型", Category: "agent", CategoryTitle: "人工智能", Aliases: []string{"AI", "大模型", "模型调用"}, Tags: []string{"智能体", "推理", "模型"}, SortOrder: 10, Color: "#0ea5e9", Icon: "bot", Width: 220, Height: 72,
		ConfigSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"modelId":{"type":"integer","title":"模型","minimum":1,"x-coinsphere-ai-model":true}},"required":["modelId"],"additionalProperties":false}`),
		UISchema:     json.RawMessage(`{"ui:order":["modelId"]}`),
		InputSchema:  json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"prompt":{"type":"string","title":"提示内容","minLength":1,"maxLength":32768,"x-coinsphere-field-source":true},"data":{"type":"object","title":"结构化数据","x-coinsphere-field-source":true}},"required":["prompt","data"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"model":{"type":"string","title":"模型"},"data":{"type":"object","title":"数据"},"usage":{"type":"object","title":"用量"}},"required":["model","data","usage"],"additionalProperties":false}`),
		Pool:         sdk.PoolStream, SideEffect: sdk.SideEffectExternal, State: sdk.StateStateless,
	}, aiModelCallAction{runtime: runtime}); err != nil {
		return err
	}
	if err := registerAIRoutes(registrar, runtime); err != nil {
		return err
	}
	if err := registrar.Page(sdk.PageDescriptor{PageKey: "models", Title: "模型配置", Icon: "ri:brain-line", KeepAlive: true}); err != nil {
		return err
	}
	if err := registrar.HeaderAction(sdk.HeaderActionDescriptor{Key: "chat", Title: "智能助手", Icon: "ri:message-3-line"}); err != nil {
		return err
	}
	return registrar.RunPanel(sdk.RunPanelDescriptor{PanelKey: "calls", Title: "AI 调用", ComponentEntry: "./official/ai/ResultPage.vue", NodeTypes: []string{"official.ai.model_call"}})
}

type aiModelCallAction struct{ runtime *runtime }

func (a aiModelCallAction) Execute(ctx context.Context, request sdk.ActionRequest) (sdk.ActionResult, error) {
	var config struct {
		ModelID int64 `json:"modelId"`
	}
	var input struct {
		Prompt string         `json:"prompt"`
		Data   map[string]any `json:"data"`
	}
	if json.Unmarshal(request.Config, &config) != nil || json.Unmarshal(request.Input, &input) != nil ||
		strings.TrimSpace(input.Prompt) == "" || input.Data == nil {
		return sdk.ActionResult{}, errors.New("AI 模型调用配置或输入无效")
	}
	model, err := a.runtime.loadAssistantModel(ctx, config.ModelID, true)
	if err != nil {
		return sdk.ActionResult{}, err
	}
	content, err := json.Marshal(map[string]any{"prompt": input.Prompt, "data": input.Data})
	if err != nil || len(content) > maxAIResponseBytes {
		return sdk.ActionResult{}, errors.New("AI 模型输入不能超过 1 MiB")
	}
	payload, err := json.Marshal(map[string]any{
		"model":           model.ModelName,
		"messages":        []map[string]string{{"role": "user", "content": string(content)}},
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return sdk.ActionResult{}, errors.New("编码 AI 模型请求失败")
	}
	callCtx, cancel := context.WithTimeout(ctx, model.Timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(callCtx, http.MethodPost, model.endpoint("chat/completions"), bytes.NewReader(payload))
	if err != nil {
		return sdk.ActionResult{}, errors.New("创建 AI 模型请求失败")
	}
	model.authorize(httpRequest)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "CoinSphere-AI/1.0")
	httpRequest.Header.Set("Idempotency-Key", request.OperationKey)
	response, err := model.client(model.Timeout).Do(httpRequest)
	if err != nil {
		return sdk.ActionResult{}, &sdk.ExecutionError{Class: sdk.ErrorUnknownResult, Err: errors.New("AI 请求结果未知，须先核对结果")}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxAIResponseBytes+1))
	if err != nil || len(raw) > maxAIResponseBytes {
		return sdk.ActionResult{}, &sdk.ExecutionError{Class: sdk.ErrorUnknownResult, Err: errors.New("AI 模型响应不能超过 1 MiB")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return sdk.ActionResult{}, &sdk.ExecutionError{Class: sdk.ErrorUnknownResult, Err: fmt.Errorf("AI 模型响应状态码为 %d", response.StatusCode)}
	}
	var modelResponse struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage map[string]any `json:"usage"`
	}
	if json.Unmarshal(raw, &modelResponse) != nil || len(modelResponse.Choices) != 1 || modelResponse.Usage == nil {
		return sdk.ActionResult{}, &sdk.ExecutionError{Class: sdk.ErrorUnknownResult, Err: errors.New("AI 模型响应无效")}
	}
	var data map[string]any
	if json.Unmarshal([]byte(modelResponse.Choices[0].Message.Content), &data) != nil || data == nil {
		return sdk.ActionResult{}, &sdk.ExecutionError{Class: sdk.ErrorUnknownResult, Err: errors.New("AI 模型响应内容须为 JSON 对象")}
	}
	if modelResponse.Model == "" {
		modelResponse.Model = model.ModelName
	}
	return sdk.ActionResult{Output: mustMarshal(map[string]any{
		"model": modelResponse.Model, "data": data, "usage": modelResponse.Usage,
	})}, nil
}

var _ sdk.ActionHandler = aiModelCallAction{}

func mustMarshal(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}
