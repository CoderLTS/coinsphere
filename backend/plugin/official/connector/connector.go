package connector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"coinsphere/backend/plugin/official/internal/safehttp"
	"coinsphere/backend/plugin/sdk"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/gorilla/websocket"
)

const (
	pluginID                 = "official.connector"
	maxConnectorPayloadBytes = 1 << 20
)

var errPermanentWebSocketHandshake = errors.New("WebSocket 握手失败且不可重试")
var emptyObjectSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false}`)
var dynamicObjectSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`)

func Register(registrar sdk.Registrar, host sdk.Host) error {
	client, err := host.Network.New(host.AllowedHTTPHosts)
	if err != nil {
		return err
	}
	if err := registrar.Action(sdk.NodeDescriptor{
		Type: "official.connector.http", Version: "1.0.0", Kind: sdk.NodeKindAction,
		Title: "HTTP 请求", Description: "向外部服务发起受控 HTTP 请求", Category: "integration", CategoryTitle: "集成", Aliases: []string{"接口请求", "HTTP 调用"}, Tags: []string{"集成", "网络"}, SortOrder: 10, Color: "#0f766e", Icon: "globe", Width: 220, Height: 72,
		ConfigSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"url":{"type":"string","title":"请求地址","format":"uri","maxLength":2048},"method":{"type":"string","title":"请求方法","enum":["GET","POST","PUT","PATCH"],"default":"GET","enumLabels":["GET","POST","PUT","PATCH"]},"timeoutSeconds":{"type":"integer","title":"超时时间（秒）","minimum":1,"maximum":60,"default":15},"useAuthorization":{"type":"boolean","title":"使用访问凭据","default":false},"authorization":{"type":"string","title":"访问凭据","x-coinsphere-secret":true,"description":"仅保存加密值；留空保持原有凭据。"}},"required":["url","method","timeoutSeconds","useAuthorization"],"additionalProperties":false}`),
		UISchema:     json.RawMessage(`{"ui:order":["url","method","timeoutSeconds","useAuthorization","authorization"]}`),
		InputSchema:  json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"body":{"type":"object","title":"请求内容","x-coinsphere-field-source":true}},"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"status":{"type":"integer","title":"状态"},"data":{"type":"object","title":"数据"}},"required":["status","data"],"additionalProperties":false}`),
		Pool:         sdk.PoolStream, SideEffect: sdk.SideEffectExternal, State: sdk.StateStateless,
	}, connectorHTTPAction{client: client}); err != nil {
		return err
	}
	if err := registrar.Trigger(sdk.NodeDescriptor{
		Type: "official.connector.webhook", Version: "1.0.0", Kind: sdk.NodeKindTrigger,
		Title: "Webhook 触发", Description: "声明 Webhook 触发入口节点", Category: "start", CategoryTitle: "触发入口", Aliases: []string{"Webhook", "回调入口"}, Tags: []string{"入口", "事件", "HTTP"}, SortOrder: 50, Color: "#1e40af", Icon: "webhook", Width: 220, Height: 72,
		ConfigSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"eventType":{"type":"string","title":"事件类型","minLength":1,"maxLength":255},"secret":{"type":"string","title":"Webhook 密钥","x-coinsphere-secret":true,"description":"仅保存加密值；留空保持原有凭据。"}},"required":["eventType","secret"],"additionalProperties":false}`),
		UISchema:     json.RawMessage(`{"ui:order":["eventType","secret"]}`), InputSchema: emptyObjectSchema,
		OutputSchema: dynamicObjectSchema, Pool: sdk.PoolStream, SideEffect: sdk.SideEffectNone, State: sdk.StateStateless,
	}, webhookTrigger{}); err != nil {
		return err
	}
	if err := registrar.Trigger(sdk.NodeDescriptor{
		Type: "official.connector.websocket", Version: "1.0.0", Kind: sdk.NodeKindTrigger,
		Title: "WebSocket 触发", Description: "从 WebSocket 消息触发工作流", Category: "start", CategoryTitle: "触发入口", Aliases: []string{"WebSocket", "长连接入口"}, Tags: []string{"入口", "事件", "实时"}, SortOrder: 60, Color: "#0f766e", Icon: "radio", Width: 220, Height: 72,
		ConfigSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"url":{"type":"string","title":"WebSocket 连接地址","format":"uri","maxLength":2048},"eventType":{"type":"string","title":"事件类型","minLength":1,"maxLength":255},"idField":{"type":"string","title":"事件编号字段","pattern":"^[A-Za-z0-9_.-]{1,128}$"},"partitionField":{"type":"string","title":"分区字段","pattern":"^[A-Za-z0-9_.-]{1,128}$"},"useAuthorization":{"type":"boolean","title":"使用访问凭据","default":false},"authorization":{"type":"string","title":"访问凭据","x-coinsphere-secret":true,"description":"仅保存加密值；留空保持原有凭据。"}},"required":["url","eventType","idField","partitionField","useAuthorization"],"additionalProperties":false}`),
		UISchema:     json.RawMessage(`{"ui:order":["url","eventType","idField","partitionField","useAuthorization","authorization"]}`), InputSchema: emptyObjectSchema,
		OutputSchema: dynamicObjectSchema, Pool: sdk.PoolStream, SideEffect: sdk.SideEffectExternal, State: sdk.StateStateless,
	}, websocketTrigger{client: client}); err != nil {
		return err
	}
	if err := registrar.Ingress("official.connector.webhook", receiveWebhook); err != nil {
		return err
	}
	return registrar.RunPanel(sdk.RunPanelDescriptor{PanelKey: "connections", Title: "连接诊断", ComponentEntry: "./official/connector/ResultPage.vue", NodeTypes: []string{"official.connector.http"}})
}

type connectorHTTPAction struct{ client sdk.NetworkClient }

func (connectorHTTPAction) RetrySafe(raw json.RawMessage) bool {
	var config struct {
		Method string `json:"method"`
	}
	return json.Unmarshal(raw, &config) == nil && config.Method == http.MethodGet
}

func (a connectorHTTPAction) Execute(ctx context.Context, request sdk.ActionRequest) (sdk.ActionResult, error) {
	var config struct {
		URL              string `json:"url"`
		Method           string `json:"method"`
		TimeoutSeconds   int    `json:"timeoutSeconds"`
		UseAuthorization bool   `json:"useAuthorization"`
	}
	var input struct {
		Body map[string]any `json:"body"`
	}
	if json.Unmarshal(request.Config, &config) != nil || json.Unmarshal(request.Input, &input) != nil {
		return sdk.ActionResult{}, errors.New("HTTP 连接器配置无效")
	}
	target, err := url.ParseRequestURI(config.URL)
	if err != nil || !target.IsAbs() {
		return sdk.ActionResult{}, errors.New("HTTP 连接器地址无效")
	}
	if config.UseAuthorization && safehttp.IsBinanceDomain(target.Hostname()) {
		return sdk.ActionResult{}, safehttp.Blocked("generic connectors cannot authorize Binance requests")
	}
	var body io.Reader
	if config.Method != http.MethodGet && input.Body != nil {
		raw, err := json.Marshal(input.Body)
		if err != nil || len(raw) > maxConnectorPayloadBytes {
			return sdk.ActionResult{}, errors.New("HTTP 连接器请求体不能超过 1 MiB")
		}
		body = bytes.NewReader(raw)
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutSeconds)*time.Second)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(callCtx, config.Method, target.String(), body)
	if err != nil {
		return sdk.ActionResult{}, errors.New("创建 HTTP 连接器请求失败")
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("User-Agent", "CoinSphere-Connector/1.0")
	if config.Method != http.MethodGet {
		httpRequest.Header.Set("Idempotency-Key", request.OperationKey)
	}
	if config.UseAuthorization {
		authorization, err := request.Secrets.Read(callCtx, "authorization")
		if err != nil || len(authorization) == 0 || len(authorization) > 16<<10 {
			return sdk.ActionResult{}, errors.New("连接器认证凭据不可用")
		}
		httpRequest.Header.Set("Authorization", string(authorization))
	}
	response, err := a.client.Do(httpRequest)
	if err != nil {
		class := sdk.ErrorUnknownResult
		if config.Method == http.MethodGet {
			class = sdk.ErrorTransient
		}
		return sdk.ActionResult{}, &sdk.ExecutionError{Class: class, Err: errors.New("HTTP 连接器请求未返回结果")}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxConnectorPayloadBytes+1))
	if err != nil || len(raw) > maxConnectorPayloadBytes {
		class := sdk.ErrorUnknownResult
		if config.Method == http.MethodGet {
			class = sdk.ErrorPermanent
		}
		return sdk.ActionResult{}, &sdk.ExecutionError{Class: class, Err: errors.New("HTTP 连接器响应无法读取或超过 1 MiB")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return sdk.ActionResult{}, fmt.Errorf("HTTP 连接器响应状态码为 %d", response.StatusCode)
	}
	var data map[string]any
	if json.Unmarshal(raw, &data) != nil || data == nil {
		return sdk.ActionResult{}, errors.New("HTTP 连接器响应须为 JSON 对象")
	}
	return sdk.ActionResult{Output: mustMarshal(map[string]any{"status": response.StatusCode, "data": data})}, nil
}

type webhookTrigger struct{}

func (webhookTrigger) Run(ctx context.Context, _ sdk.TriggerRequest, _ sdk.Emitter) error {
	<-ctx.Done()
	return ctx.Err()
}

type websocketTrigger struct{ client sdk.NetworkClient }

func (t websocketTrigger) Run(ctx context.Context, request sdk.TriggerRequest, emitter sdk.Emitter) error {
	var config struct {
		URL, EventType, IDField, PartitionField string
		UseAuthorization                        bool `json:"useAuthorization"`
	}
	if json.Unmarshal(request.Config, &config) != nil {
		return errors.New("WebSocket 连接器配置无效")
	}
	target, err := url.ParseRequestURI(config.URL)
	if err != nil || !target.IsAbs() {
		return errors.New("WebSocket 连接器地址无效")
	}
	if err := t.client.ValidateWebSocketURL(ctx, target, config.UseAuthorization); err != nil {
		return err
	}
	headers := http.Header{"User-Agent": []string{"CoinSphere-Connector/1.0"}}
	if config.UseAuthorization {
		authorization, err := request.Secrets.Read(ctx, "authorization")
		if err != nil || len(authorization) == 0 || len(authorization) > 16<<10 {
			return errors.New("连接器认证凭据不可用")
		}
		headers.Set("Authorization", string(authorization))
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := t.readWebSocket(ctx, request, emitter, target, headers, config.EventType, config.IDField, config.PartitionField); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, errPermanentWebSocketHandshake) {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
}

func (t websocketTrigger) readWebSocket(ctx context.Context, request sdk.TriggerRequest, emitter sdk.Emitter, target *url.URL, headers http.Header, eventType, idField, partitionField string) error {
	if err := t.client.ValidateWebSocketURL(ctx, target, headers.Get("Authorization") != ""); err != nil {
		return err
	}
	dialer := websocket.Dialer{Proxy: nil, NetDialContext: t.client.DialContext, HandshakeTimeout: 10 * time.Second}
	connection, response, err := dialer.DialContext(ctx, target.String(), headers)
	status := 0
	if response != nil {
		status = response.StatusCode
		if response.Body != nil {
			response.Body.Close()
		}
	}
	if err != nil {
		if safehttp.PermanentWebSocketStatus(status) {
			return fmt.Errorf("%w：状态码 %d", errPermanentWebSocketHandshake, status)
		}
		return err
	}
	connection.SetReadLimit(maxConnectorPayloadBytes)
	closed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			connection.Close()
		case <-closed:
		}
	}()
	defer close(closed)
	defer connection.Close()
	for {
		var data map[string]any
		if err := connection.ReadJSON(&data); err != nil {
			return err
		}
		eventID, ok := connectorStringField(data, idField, 128)
		if !ok {
			return errors.New("WebSocket 连接器事件编号字段缺失或无效")
		}
		partitionKey, ok := connectorStringField(data, partitionField, 256)
		if !ok {
			return errors.New("WebSocket 连接器分区字段缺失或无效")
		}
		event := cloudevents.NewEvent()
		event.SetID(eventID)
		event.SetSource(fmt.Sprintf("urn:coinsphere:connector:websocket:%s:%s", request.Revision.WorkflowID, request.NodeInstanceID))
		event.SetType(eventType)
		event.SetTime(time.Now().UTC())
		event.SetExtension("partitionkey", partitionKey)
		if err := event.SetData(cloudevents.ApplicationJSON, data); err != nil {
			return errors.New("编码 WebSocket 连接器事件失败")
		}
		if err := emitter.Emit(ctx, event); err != nil {
			return err
		}
	}
}

func connectorStringField(data map[string]any, field string, limit int) (string, bool) {
	var current any = data
	for _, segment := range strings.Split(field, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = object[segment]
		if !ok {
			return "", false
		}
	}
	value, ok := current.(string)
	value = strings.TrimSpace(value)
	return value, ok && value != "" && len(value) <= limit
}

func mustMarshal(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

var _ sdk.ActionHandler = connectorHTTPAction{}
var _ sdk.TriggerHandler = webhookTrigger{}
var _ sdk.TriggerHandler = websocketTrigger{}

func receiveWebhook(ctx context.Context, request sdk.IngressRequest) (cloudevents.Event, error) {
	readHeader := func(name string) (string, bool) {
		v := request.Request.Header.Values(name)
		if len(v) != 1 {
			return "", false
		}
		return v[0], strings.TrimSpace(v[0]) != ""
	}
	secret, ok := readHeader("X-CoinSphere-Webhook-Secret")
	id, idOK := readHeader("Idempotency-Key")
	partition, partitionOK := readHeader("X-CoinSphere-Partition-Key")
	if !ok || !idOK || !partitionOK || len(secret) > 16384 || len(id) > 128 || len(partition) > 256 {
		return cloudevents.Event{}, errors.New("入站请求头无效")
	}
	expected, err := request.Secrets.Read(ctx, "secret")
	if err != nil {
		return cloudevents.Event{}, err
	}
	a, b := sha256.Sum256(expected), sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
		return cloudevents.Event{}, errors.New("入站认证无效")
	}
	var config struct {
		EventType string `json:"eventType"`
	}
	var data map[string]any
	if json.Unmarshal(request.Config, &config) != nil || json.Unmarshal(request.Data, &data) != nil || data == nil {
		return cloudevents.Event{}, errors.New("入站数据无效")
	}
	event := cloudevents.NewEvent()
	event.SetID(strings.TrimSpace(id))
	event.SetSource("urn:coinsphere:connector:webhook:" + request.Revision.WorkflowID)
	event.SetType(config.EventType)
	event.SetTime(request.EventTime.UTC())
	event.SetExtension("partitionkey", strings.TrimSpace(partition))
	err = event.SetData(cloudevents.ApplicationJSON, data)
	return event, err
}
