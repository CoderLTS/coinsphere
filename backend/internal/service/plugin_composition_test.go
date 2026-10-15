package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"coinsphere/backend/internal/db"
	"coinsphere/backend/plugin/official"
	"coinsphere/backend/plugin/sdk"
	"coinsphere/backend/version"
	workflowgraph "coinsphere/backend/workflow/graph"
	cloudevents "github.com/cloudevents/sdk-go/v2"
)

type compositionNetwork struct {
	sdk.NetworkClient
	t    *testing.T
	sent []map[string]any
}

func (n *compositionNetwork) New([]string) (sdk.NetworkClient, error) { return n, nil }
func (*compositionNetwork) SetTimeout(time.Duration)                  {}
func (*compositionNetwork) DisableRedirects()                         {}
func (n *compositionNetwork) DoProxied(r *http.Request, _ *url.URL) (*http.Response, error) {
	return n.Do(r)
}
func (n *compositionNetwork) Do(r *http.Request) (*http.Response, error) {
	raw := ""
	switch r.URL.Host + r.URL.Path {
	case "feed.example/data":
		raw = `{"value":"collected"}`
	case "bots.qq.com/app/getAppAccessToken":
		raw = `{"access_token":"synthetic-qq-token","expires_in":3600}`
	case "api.sgroup.qq.com/v2/groups/synthetic-group/messages":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			n.t.Fatal(err)
		}
		n.sent = append(n.sent, body)
		raw = `{"id":"synthetic-reply"}`
	case "data-api.binance.vision/api/v3/ticker/price":
		raw = `{"symbol":"BTCUSDT","price":"100"}`
	default:
		n.t.Fatalf("unexpected external operation: %s", r.URL.Host+r.URL.Path)
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Header: http.Header{}}, nil
}

func compositionFixture(t *testing.T) (*App, context.Context, *Principal, *compositionNetwork) {
	t.Helper()
	a, ctx, p := platformFixture(t, nil)
	network := &compositionNetwork{t: t}
	enabled := map[string]bool{}
	for _, plugin := range version.BuiltinCatalog {
		enabled[plugin.ID] = true
	}
	if err := official.RegisterAll(a.Plugins, sdk.Host{Platform: a, Sessions: a, Credentials: a.Cipher, Stores: sdk.GormPluginStores{Database: a.DB}, Network: network, Inbox: a, Events: a, Realtime: a, OutboundProxy: a}, enabled); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if r.URL.Path != "/v1/chat/completions" || json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) != 1 {
			t.Error("invalid AI composition request")
			w.WriteHeader(400)
			return
		}
		var input struct {
			Prompt string         `json:"prompt"`
			Data   map[string]any `json:"data"`
		}
		if json.Unmarshal([]byte(request.Messages[0].Content), &input) != nil || input.Prompt == "" || input.Data == nil {
			t.Error("AI lost structured upstream input")
			w.WriteHeader(400)
			return
		}
		value := input.Data["value"]
		if value == nil {
			value = input.Prompt
		}
		content, _ := json.Marshal(map[string]any{"message": value})
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "synthetic", "choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}}, "usage": map[string]any{"total_tokens": 1}})
	}))
	t.Cleanup(server.Close)
	if err := a.DB.Exec(`INSERT INTO plugin_ai.ai_model_configs(id,display_name,base_url,model_name,created_by,updated_by) VALUES(1,'Synthetic',?,'synthetic',1,1)`, server.URL+"/v1").Error; err != nil {
		t.Fatal(err)
	}
	return a, ctx, p, network
}

func compositionNode(id, typ, config string, bindings map[string]workflowInputBinding) workflowGraphNode {
	return workflowGraphNode{NodeInstanceID: id, NodeType: typ, NodeVersion: "1.0.0", Config: json.RawMessage(config), Position: &workflowgraph.Position{}, InputBindings: bindings}
}
func compositionLiteral(value any) workflowInputBinding {
	return workflowInputBinding{Kind: "literal", Value: mustJSON(value)}
}
func compositionField(node string, path ...string) workflowInputBinding {
	return workflowInputBinding{Kind: "field", NodeInstanceID: node, FieldPath: path}
}
func compositionGraph(nodes ...workflowGraphNode) workflowGraph {
	g := workflowGraph{SchemaVersion: 3, EntryPoints: map[string]string{"main": nodes[0].NodeInstanceID}, Nodes: nodes}
	for i := 1; i < len(nodes); i++ {
		g.Edges = append(g.Edges, workflowGraphEdge{EdgeID: fmt.Sprint("e", i), SourceNodeInstanceID: nodes[i-1].NodeInstanceID, SourcePort: "out", TargetNodeInstanceID: nodes[i].NodeInstanceID, TargetPort: "in"})
	}
	return g
}
func executeComposition(t *testing.T, a *App, ctx context.Context) db.WorkflowRun {
	t.Helper()
	run := claimTestRun(t, a)
	a.executeWorkflowRun(ctx, run)
	if err := a.DB.First(&run, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusSucceeded {
		t.Fatalf("composition failed: %s / %v", run.Status, run.ErrorMessage)
	}
	return run
}

func TestConnectorAIInboxComposition(t *testing.T) {
	a, ctx, p, _ := compositionFixture(t)
	g := compositionGraph(
		compositionNode("manual", "core.manual", `{}`, nil),
		compositionNode("collect", "official.connector.http", `{"url":"https://feed.example/data","method":"GET","timeoutSeconds":15,"useAuthorization":false}`, map[string]workflowInputBinding{"body": compositionLiteral(map[string]any{})}),
		compositionNode("ai", "official.ai.model_call", `{"modelId":1}`, map[string]workflowInputBinding{"prompt": compositionLiteral("summarize"), "data": compositionField("collect", "data")}),
		compositionNode("notice", "official.notification.in_app", `{"title":"Collected"}`, map[string]workflowInputBinding{"subjectKey": compositionLiteral("collected"), "message": compositionField("ai", "data", "message")}),
		compositionNode("end", "core.end", `{}`, nil),
	)
	workflow := createTestWorkflow(t, a, ctx, p, g)
	queueTestRun(t, a, ctx, p, workflow)
	executeComposition(t, a, ctx)
	var notice db.NotificationDelivery
	if err := a.DB.First(&notice).Error; err != nil || notice.Message != "collected" || notice.RecipientUserID == nil || *notice.RecipientUserID != p.User.ID {
		t.Fatal("structured data did not reach personal inbox", err)
	}
}

func TestQQAIReplyComposition(t *testing.T) {
	a, ctx, p, network := compositionFixture(t)
	g := compositionGraph(
		compositionNode("receive", "official.qq.receive", `{"appId":"synthetic-app"}`, nil),
		compositionNode("ai", "official.ai.model_call", `{"modelId":1}`, map[string]workflowInputBinding{"prompt": compositionField("receive", "content"), "data": compositionLiteral(map[string]any{})}),
		compositionNode("reply", "official.qq.send", `{"appId":"synthetic-app"}`, map[string]workflowInputBinding{"subjectKey": compositionField("receive", "messageId"), "targetType": compositionField("receive", "targetType"), "targetId": compositionField("receive", "targetId"), "messageType": compositionLiteral("text"), "content": compositionField("ai", "data", "message"), "replyToMessageId": compositionField("receive", "messageId"), "replySequence": compositionLiteral(1)}),
		compositionNode("end", "core.end", `{}`, nil),
	)
	secret := "synthetic-qq-secret"
	workflow, err := a.CreateWorkflow(ctx, WorkflowCreatePayload{Name: "QQ synthetic", Graph: mustJSON(g), SecretChanges: []WorkflowSecretChange{{NodeInstanceID: "receive", Field: "clientSecret", Value: &secret}, {NodeInstanceID: "reply", Field: "clientSecret", Value: &secret}}}, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.PublishWorkflowRevision(ctx, workflow.ID, WorkflowPublishPayload{RevisionID: workflow.DraftRevisionID}, p); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ApplyWorkflowLifecycle(ctx, workflow.ID, WorkflowLifecyclePayload{Action: "activate"}); err != nil {
		t.Fatal(err)
	}
	// 外部网关在已规范化的消息边界替换，后续使用真实图绑定、节点和投递事务。
	event := cloudevents.NewEvent()
	event.SetID("synthetic-message")
	event.SetSource("urn:coinsphere:qq:synthetic-app")
	event.SetType("GROUP_AT_MESSAGE_CREATE")
	event.SetTime(time.Now().UTC())
	event.SetExtension("partitionkey", "synthetic-group")
	if err = event.SetData(cloudevents.ApplicationJSON, map[string]any{"eventType": "GROUP_AT_MESSAGE_CREATE", "targetType": "group", "targetId": "synthetic-group", "senderOpenId": "synthetic-user", "messageId": "synthetic-message", "messageType": 0, "content": "reply to this", "timestamp": event.Time().Format(time.RFC3339Nano), "attachments": []any{}}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.persistWorkflowEvent(ctx, event, workflow.ID); err != nil {
		t.Fatal(err)
	}
	executeComposition(t, a, ctx)
	if len(network.sent) != 1 || network.sent[0]["content"] != "reply to this" || network.sent[0]["msg_id"] != "synthetic-message" {
		t.Fatal("QQ reply lost upstream content or target")
	}
	if _, err = a.persistWorkflowEvent(ctx, event, workflow.ID); err != nil {
		t.Fatal(err)
	}
	if countTestRows(t, a, "workflow_runs", "workflow_id=?", workflow.ID) != 1 || countTestRows(t, a, "plugin_notification.deliveries", "channel='qq' AND status='delivered'") != 1 {
		t.Fatal("QQ duplicate event or delivery was repeated")
	}
}

func TestMarketStrategyPaperInboxComposition(t *testing.T) {
	a, ctx, p, _ := compositionFixture(t)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := a.DB.Exec(`INSERT INTO plugin_binance.candles(market,instrument,interval,open_time,close_time,open,high,low,close,volume,source_event_id) VALUES('spot','BTCUSDT','1h',?,?,?,200,1,?,1,?)`, start.Add(time.Duration(i)*time.Hour), start.Add(time.Duration(i+1)*time.Hour), 100+i, 100+i, fmt.Sprint("synthetic-candle-", i)).Error; err != nil {
			t.Fatal(err)
		}
	}
	g := compositionGraph(
		compositionNode("manual", "core.manual", `{}`, nil),
		compositionNode("strategy", "official.quant.evaluate", `{"venue":"binance","strategyId":"official.quant.sma-crossover","market":"spot","instrument":"BTCUSDT","interval":"1h","parameters":{"fastPeriod":1,"slowPeriod":3}}`, map[string]workflowInputBinding{"eventTime": compositionLiteral("2026-01-01T03:00:00Z")}),
		compositionNode("condition", "core.condition", `{"match":"all","rules":[{"fieldPath":["target"],"operator":"eq","value":"1"}]}`, map[string]workflowInputBinding{"target": compositionField("strategy", "target")}),
		compositionNode("intent", "official.quant.order_intent", `{"venue":"binance","account":"synthetic-paper","market":"spot","instrument":"BTCUSDT","quantity":"0","quoteAmount":"10","positionEffect":"open"}`, map[string]workflowInputBinding{"action": compositionLiteral("buy"), "evaluatedAt": compositionField("strategy", "evaluatedAt")}),
		compositionNode("paper", "official.binance.paper_execute", `{"initialBalance":"100","feeRate":"0.001","maxOrderNotional":"20","maxInstrumentNotional":"20"}`, nil),
		compositionNode("notice", "official.notification.in_app", `{"title":"Paper completed"}`, map[string]workflowInputBinding{"subjectKey": compositionField("intent", "clientOrderId"), "message": compositionLiteral("Paper completed from real strategy")}),
		compositionNode("end", "core.end", `{}`, nil),
	)
	g.Nodes[4].InputBindings = map[string]workflowInputBinding{}
	for _, key := range []string{"venue", "account", "market", "instrument", "side", "quantity", "quoteAmount", "positionEffect", "clientOrderId", "referencePrice", "quotedAt"} {
		g.Nodes[4].InputBindings[key] = compositionField("intent", key)
	}
	g.Edges[2].SourcePort = "true"
	for _, port := range []string{"false", "unavailable"} {
		g.Edges = append(g.Edges, workflowGraphEdge{EdgeID: port, SourceNodeInstanceID: "condition", SourcePort: port, TargetNodeInstanceID: "end", TargetPort: "in"})
	}
	workflow := createTestWorkflow(t, a, ctx, p, g)
	queueTestRun(t, a, ctx, p, workflow)
	executeComposition(t, a, ctx)
	if countTestRows(t, a, "plugin_binance.orders", "workflow_id=? AND mode='paper' AND status='filled'", workflow.ID) != 1 || countTestRows(t, a, "notification_inbox", "workflow_id=?", workflow.ID) != 1 {
		t.Fatal("strategy did not produce a Paper order and personal notification")
	}
	var balance string
	if err := a.DB.Raw(`SELECT sum(amount)::text FROM plugin_binance.paper_ledger_entries WHERE account='synthetic-paper'`).Scan(&balance).Error; err != nil || balance != "89.990000000000000000" {
		t.Fatal("Paper accounting changed in composition", balance, err)
	}
}
