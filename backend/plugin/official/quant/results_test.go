package quant

import (
	"coinsphere/backend/internal/testdb"
	"coinsphere/backend/plugin/sdk"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDirectBacktestsDefaultFilterDetailAndScope(t *testing.T) {
	_, database := testdb.Open(t, true)
	runtime := &quantRuntime{db: database}
	for i := int64(1); i <= 2; i++ {
		row := quantBacktest{OperationKey: string(rune('a' + i)), WorkflowID: i, RevisionID: 1, NodeInstanceID: "backtest", StrategyID: "synthetic", StrategyVersion: "1.0.0", Venue: "test", Market: "spot", Instrument: "BTCUSDT", Interval: "1h", StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), InitialCapital: decimal.NewFromInt(100), FinalEquity: decimal.RequireFromString("100.123456789012345678"), CandleCount: 1, Parameters: "{}", DataManifest: "{}", Detail: `{"schemaVersion":2}`, CreatedAt: time.Now().UTC()}
		if err := database.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		scope, filters, path string
		count, status        int
	}{
		{"{}", "{}", "/backtests", 2, 200},
		{`{"workflowId":1,"nodeInstanceId":"backtest"}`, "{}", "/backtests", 1, 200},
		{"{}", `{"instrument":"ETHUSDT"}`, "/backtests?instrument=ETHUSDT", 0, 200},
		{"{}", "{}", "/backtests?unknown=1", 0, 400},
	} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)
		runtime.handleQuantBacktests(c, sdk.ResultScope{PluginID: quantPluginID, PageKey: "backtests", UserID: 1, Scope: json.RawMessage(tc.scope), Filters: json.RawMessage(tc.filters)})
		if recorder.Code != tc.status {
			t.Fatal(recorder.Code, recorder.Body.String())
		}
		if tc.status != 200 {
			continue
		}
		var envelope struct {
			Data struct{ Items []map[string]any }
		}
		if json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil || len(envelope.Data.Items) != tc.count {
			t.Fatal(recorder.Body.String())
		}
		for _, item := range envelope.Data.Items {
			if item["finalEquity"] != "100.123456789012345678" {
				t.Fatal("Decimal result lost precision", item)
			}
		}
	}
	for _, workflow := range []int{1, 2} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/backtests/1", nil)
		c.Params = gin.Params{{Key: "backtestId", Value: "1"}}
		raw := json.RawMessage(`{"workflowId":1}`)
		if workflow == 2 {
			raw = json.RawMessage(`{"workflowId":2}`)
		}
		runtime.handleQuantBacktest(c, sdk.ResultScope{PluginID: quantPluginID, PageKey: "backtests", UserID: 1, Scope: raw, Filters: json.RawMessage(`{}`)})
		expected := 200
		if workflow == 2 {
			expected = 404
		}
		if recorder.Code != expected {
			t.Fatal("detail escaped scope", recorder.Code, recorder.Body.String())
		}
	}
}
