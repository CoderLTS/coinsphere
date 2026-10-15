package quant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"coinsphere/backend/plugin/contracts/trading"
	"coinsphere/backend/plugin/sdk"
	"github.com/shopspring/decimal"
)

type orderIntentAction struct{ runtime *quantRuntime }

func (q *quantRuntime) registerOrderIntent(registrar sdk.Registrar) error {
	return registrar.Action(quantNodeMeta(sdk.NodeDescriptor{
		Type: "official.quant.order_intent", Version: "1.0.0", Kind: sdk.NodeKindAction,
		ConfigSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"venue":{"type":"string","title":"交易所","pattern":"^[a-z][a-z0-9_-]{1,31}$","default":"binance"},"account":{"type":"string","title":"账户","pattern":"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$"},"market":{"type":"string","title":"市场类型","minLength":1,"maxLength":32},"instrument":{"type":"string","title":"交易对","pattern":"^[A-Z0-9]{2,32}$"},"quantity":{"type":"string","title":"数量","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"default":"0","description":"使用十进制字符串，避免浮点精度损失。"},"quoteAmount":{"type":"string","title":"报价金额","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"default":"0","description":"使用十进制字符串，避免浮点精度损失。"},"positionEffect":{"type":"string","title":"持仓操作","enum":["open","reduce"],"default":"open","enumLabels":["开仓","减仓"]}},"required":["venue","account","market","instrument","quantity","quoteAmount","positionEffect"],"additionalProperties":false}`),
		UISchema:     json.RawMessage(`{"ui:order":["venue","account","market","instrument","quantity","quoteAmount","positionEffect"]}`),
		InputSchema:  json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"action":{"type":"string","enum":["buy","sell","hold"],"x-coinsphere-field-source":true,"title":"操作","enumLabels":["买入","卖出","观望"]},"evaluatedAt":{"type":"string","format":"date-time","x-coinsphere-field-source":true,"title":"评估时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"}},"required":["action","evaluatedAt"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"venue":{"type":"string","title":"交易所"},"account":{"type":"string","title":"模拟账户"},"market":{"type":"string","title":"市场"},"instrument":{"type":"string","title":"品种"},"side":{"type":"string","enum":["buy","sell"],"title":"方向","enumLabels":["买入","卖出"]},"quantity":{"type":"string","x-coinsphere-decimal":true,"title":"数量","description":"使用十进制字符串，避免浮点精度损失。"},"quoteAmount":{"type":"string","x-coinsphere-decimal":true,"title":"计价金额","description":"使用十进制字符串，避免浮点精度损失。"},"positionEffect":{"type":"string","enum":["open","reduce"],"title":"仓位操作","enumLabels":["开仓","减仓"]},"clientOrderId":{"type":"string","title":"客户端订单编号"},"referencePrice":{"type":"string","x-coinsphere-decimal":true,"title":"参考价格","description":"使用十进制字符串，避免浮点精度损失。"},"quotedAt":{"type":"string","format":"date-time","title":"报价时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"}},"required":["venue","account","market","instrument","side","quantity","quoteAmount","positionEffect","clientOrderId","referencePrice","quotedAt"],"additionalProperties":false}`),
		Pool:         sdk.PoolCompute, SideEffect: sdk.SideEffectNone, State: sdk.StateStateless,
	}, "生成订单意图", "将通用量化信号转换为不含交易所私有参数的订单意图。", "strategy", "#2563eb", "file-output"), orderIntentAction{runtime: q})
}

func (a orderIntentAction) Execute(ctx context.Context, request sdk.ActionRequest) (sdk.ActionResult, error) {
	var config struct{ Venue, Account, Market, Instrument, Quantity, QuoteAmount, PositionEffect string }
	var input struct{ Action, EvaluatedAt string }
	if json.Unmarshal(request.Config, &config) != nil || json.Unmarshal(request.Input, &input) != nil || input.Action == "hold" {
		return sdk.ActionResult{}, errors.New("量化订单意图需要买入或卖出信号")
	}
	quantity, quantityErr := decimal.NewFromString(config.Quantity)
	quoteAmount, quoteErr := decimal.NewFromString(config.QuoteAmount)
	if quantityErr != nil || quoteErr != nil || quantity.Sign() < 0 || quoteAmount.Sign() < 0 || quantity.Sign() == 0 && quoteAmount.Sign() == 0 || quantity.Sign() > 0 && quoteAmount.Sign() > 0 {
		return sdk.ActionResult{}, errors.New("量化订单意图须指定且仅指定一个大于零的数量")
	}
	if a.runtime.marketData == nil {
		return sdk.ActionResult{}, errors.New("量化行情注册表不可用")
	}
	provider, ok := a.runtime.marketData.MarketDataProvider(strings.ToLower(config.Venue))
	if !ok {
		return sdk.ActionResult{}, errors.New("量化行情数据源不可用")
	}
	quote, err := provider.Quote(ctx, trading.QuoteQuery{Market: strings.ToLower(config.Market), Instrument: strings.ToUpper(config.Instrument)})
	if err != nil {
		return sdk.ActionResult{}, err
	}
	digest := sha256.Sum256([]byte(request.OperationKey))
	clientOrderID := "cs-" + hex.EncodeToString(digest[:])[:28]
	return sdk.ActionResult{Output: mustMarshal(map[string]any{
		"venue": strings.ToLower(config.Venue), "account": strings.TrimSpace(config.Account), "market": strings.ToLower(config.Market),
		"instrument": strings.ToUpper(config.Instrument), "side": input.Action, "quantity": quantity.String(), "quoteAmount": quoteAmount.String(),
		"positionEffect": config.PositionEffect, "clientOrderId": clientOrderID, "referencePrice": quote.Price.String(),
		"quotedAt": quote.QuotedAt.UTC().Format(time.RFC3339Nano),
	})}, nil
}

var _ sdk.ActionHandler = orderIntentAction{}
