package binance

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"coinsphere/backend/internal/testdb"
	"coinsphere/backend/plugin/sdk"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type syntheticQuoteClient struct {
	sdk.NetworkClient
	calls int
	t     *testing.T
}

func (s *syntheticQuoteClient) DoProxied(r *http.Request, _ *url.URL) (*http.Response, error) {
	s.calls++
	if r.URL.Path != "/api/v3/ticker/price" {
		s.t.Fatal("unexpected public operation")
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"symbol":"BTCUSDT","price":"0.125"}`))}, nil
}

func TestPaperExactLedgerIdempotenceAndOrderScope(t *testing.T) {
	_, database := testdb.Open(t, true)
	client := &syntheticQuoteClient{t: t}
	runtime := &binanceRuntime{db: database, client: client}
	request := sdk.ActionRequest{Revision: sdk.RevisionRef{WorkflowID: "7", RevisionID: "11"}, NodeInstanceID: "paper", OperationKey: strings.Repeat("a", 64), Config: json.RawMessage(`{"initialBalance":"1000.123456789012345678","feeRate":"0.001","maxOrderNotional":"100","maxInstrumentNotional":"100"}`), Input: json.RawMessage(`{"venue":"binance","account":"synthetic-paper","market":"spot","instrument":"BTCUSDT","side":"buy","quoteAmount":"1.25","positionEffect":"open","clientOrderId":"synthetic-buy-1"}`), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	result, err := (paperExecuteAction{runtime}).Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := (paperExecuteAction{runtime}).Execute(context.Background(), request)
	if err != nil || string(result.Output) != string(duplicate.Output) || client.calls != 1 {
		t.Fatal("Paper repeated a committed operation", err)
	}
	var balance decimal.Decimal
	if err := database.Model(&paperLedgerEntry{}).Select("coalesce(sum(amount),0)").Where("account=?", "synthetic-paper").Scan(&balance).Error; err != nil || !balance.Equal(decimal.RequireFromString("998.872206789012345678")) {
		t.Fatal("Paper cash lost Decimal precision", balance, err)
	}
	var ledger int64
	database.Model(&paperLedgerEntry{}).Count(&ledger)
	if ledger != 3 {
		t.Fatal("Paper retry duplicated ledger entries")
	}
	second := request
	second.Revision.WorkflowID = "8"
	second.OperationKey = strings.Repeat("b", 64)
	second.Input = json.RawMessage(`{"venue":"binance","account":"other-paper","market":"spot","instrument":"BTCUSDT","side":"buy","quoteAmount":"1.25","positionEffect":"open","clientOrderId":"synthetic-buy-2"}`)
	if _, err := (paperExecuteAction{runtime}).Execute(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]int64{{7}, {}} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/orders", nil)
		runtime.handleOrders(c, sdk.SystemScope{PluginID: pluginID, UserID: 2, WorkflowIDs: ids})
		var envelope struct {
			Data struct {
				Items []json.RawMessage `json:"items"`
			} `json:"data"`
		}
		if json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil || recorder.Code != 200 || len(envelope.Data.Items) != len(ids) {
			t.Fatal("order scope escaped its workflow list")
		}
	}
	for _, tc := range []struct {
		scope   paperResultScope
		filters paperResultFilters
		count   int
	}{
		{paperResultScope{}, paperResultFilters{}, 2},
		{paperResultScope{WorkflowID: 7}, paperResultFilters{}, 1},
		{paperResultScope{WorkflowID: 7, PaperNodeInstanceID: "missing"}, paperResultFilters{}, 0},
		{paperResultScope{}, paperResultFilters{Instrument: "ETHUSDT"}, 0},
	} {
		orders, accounts, err := runtime.paperResult(context.Background(), tc.scope, tc.filters)
		if err != nil || len(orders) != tc.count || len(accounts) != tc.count {
			t.Fatal("direct Paper query escaped scope", len(orders), len(accounts), err)
		}
		for _, order := range orders {
			if _, ok := order["averagePrice"].(string); !ok {
				t.Fatal("Paper result lost Decimal wire format")
			}
		}
	}
	var output map[string]any
	_ = json.Unmarshal(result.Output, &output)
	if _, ok := output["executed"].(string); !ok {
		t.Fatal("financial wire values must remain strings")
	}
}
