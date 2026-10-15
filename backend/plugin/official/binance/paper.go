package binance

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"coinsphere/backend/plugin/contracts/trading"
	"coinsphere/backend/plugin/sdk"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func registerPaper(registrar sdk.Registrar, runtime *binanceRuntime) error {
	return registrar.Action(withNodeMeta(sdk.NodeDescriptor{
		Type: "official.binance.paper_execute", Version: "1.0.0", Kind: sdk.NodeKindAction,
		ConfigSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"initialBalance":{"type":"string","title":"初始余额","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"description":"使用十进制字符串，避免浮点精度损失。"},"feeRate":{"type":"string","title":"手续费率","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"description":"使用十进制字符串，避免浮点精度损失。"},"maxOrderNotional":{"type":"string","title":"最大订单名义金额","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"description":"使用十进制字符串，避免浮点精度损失。"},"maxInstrumentNotional":{"type":"string","title":"单交易对最大名义金额","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"description":"使用十进制字符串，避免浮点精度损失。"}},"required":["initialBalance","feeRate","maxOrderNotional","maxInstrumentNotional"],"additionalProperties":false}`),
		UISchema:     json.RawMessage(`{"ui:order":["initialBalance","feeRate","maxOrderNotional","maxInstrumentNotional"]}`),
		InputSchema:  orderIntentSchema(), OutputSchema: orderResultSchema(), Pool: sdk.PoolStream, SideEffect: sdk.SideEffectData, RetrySafe: true, State: sdk.StatePersistent,
	}, "Paper 执行", "使用 Binance 最新 Quote 模拟成交并记录订单、成交和持仓。", "strategy", "#2563eb", "flask-conical"), paperExecuteAction{runtime: runtime})
}

type paperExecuteAction struct{ runtime *binanceRuntime }

func (a paperExecuteAction) Execute(ctx context.Context, request sdk.ActionRequest) (sdk.ActionResult, error) {
	var config struct{ InitialBalance, FeeRate, MaxOrderNotional, MaxInstrumentNotional string }
	var intent struct{ Venue, Account, Market, Instrument, Side, Quantity, QuoteAmount, PositionEffect, ClientOrderID string }
	if json.Unmarshal(request.Config, &config) != nil || json.Unmarshal(request.Input, &intent) != nil || intent.Venue != "binance" {
		return sdk.ActionResult{}, errors.New("币安模拟订单无效")
	}
	initialBalance, e1 := decimal.NewFromString(config.InitialBalance)
	feeRate, e2 := decimal.NewFromString(config.FeeRate)
	maxOrder, e3 := decimal.NewFromString(config.MaxOrderNotional)
	maxInstrument, e4 := decimal.NewFromString(config.MaxInstrumentNotional)
	quantity, e5 := decimal.NewFromString(zeroIfEmpty(intent.Quantity))
	quoteAmount, e6 := decimal.NewFromString(zeroIfEmpty(intent.QuoteAmount))
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || initialBalance.Sign() <= 0 || feeRate.Sign() < 0 || maxOrder.Sign() <= 0 || maxInstrument.Sign() <= 0 {
		return sdk.ActionResult{}, errors.New("币安模拟交易限额无效")
	}
	intent.Market = strings.ToLower(strings.TrimSpace(intent.Market))
	intent.Account = strings.TrimSpace(intent.Account)
	intent.Instrument = strings.ToUpper(strings.TrimSpace(intent.Instrument))
	intent.Side = strings.ToLower(strings.TrimSpace(intent.Side))
	intent.PositionEffect = strings.ToLower(strings.TrimSpace(intent.PositionEffect))
	if !clientOrderIDPattern.MatchString(intent.ClientOrderID) || !accountIDPattern.MatchString(intent.Account) || (quantity.Sign() <= 0 && quoteAmount.Sign() <= 0) || (quantity.Sign() > 0 && quoteAmount.Sign() > 0) ||
		(intent.Market != "spot" && intent.Market != "usdm") || !instrumentPattern.MatchString(intent.Instrument) ||
		(intent.Side != "buy" && intent.Side != "sell") || (intent.PositionEffect != "open" && intent.PositionEffect != "reduce") ||
		intent.Market == "usdm" && quoteAmount.Sign() > 0 {
		return sdk.ActionResult{}, errors.New("币安模拟订单标识无效")
	}
	if existing, ok, err := a.runtime.orderByClientID(ctx, intent.ClientOrderID); err != nil {
		return sdk.ActionResult{}, err
	} else if ok {
		if !matchesOrderIntent(existing, "paper", intent.Account, intent.Market, intent.Instrument, intent.Side, intent.PositionEffect, intent.ClientOrderID, quantity, quoteAmount) {
			return sdk.ActionResult{}, errors.New("币安客户订单编号已属于其他订单意图")
		}
		return sdk.ActionResult{Output: marshalOrder(existing)}, nil
	}
	workflowID, workflowErr := strconv.ParseInt(request.Revision.WorkflowID, 10, 64)
	if workflowErr != nil || workflowID <= 0 || strings.TrimSpace(request.NodeInstanceID) == "" {
		return sdk.ActionResult{}, errors.New("币安模拟交易工作流标识无效")
	}
	requestedQuantity, requestedQuoteAmount := quantity, quoteAmount
	quote, err := (marketDataProvider{runtime: a.runtime}).Quote(ctx, trading.QuoteQuery{Market: intent.Market, Instrument: intent.Instrument})
	if err != nil || quote.Price.Sign() <= 0 {
		return sdk.ActionResult{}, errors.New("币安模拟交易报价不可用")
	}
	if quantity.Sign() <= 0 {
		quantity = quoteAmount.Div(quote.Price)
	}
	if quoteAmount.Sign() == 0 {
		if err := a.runtime.validateOrderRules(ctx, intent.Market, intent.Instrument, quantity); err != nil {
			return sdk.ActionResult{}, err
		}
	}
	notional := quantity.Mul(quote.Price)
	if notional.Sign() <= 0 || notional.GreaterThan(maxOrder) {
		return sdk.ActionResult{}, errors.New("币安模拟订单超过单笔名义金额上限")
	}
	var stored tradingOrder
	err = a.runtime.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "binance-paper:"+intent.Account).Error; err != nil {
			return errors.New("锁定币安模拟账户失败")
		}
		openingKey := paperOpeningOperationKey(intent.Account)
		opening := paperLedgerEntry{Account: intent.Account, OperationKey: openingKey, EntryType: "opening_balance", Amount: initialBalance, OccurredAt: time.Now().UTC()}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "operation_key"}, {Name: "entry_type"}}, DoNothing: true}).Create(&opening).Error; err != nil {
			return errors.New("保存币安模拟账户期初余额失败")
		}
		var persistedOpening paperLedgerEntry
		if err := tx.Where("operation_key = ? AND entry_type = 'opening_balance'", openingKey).First(&persistedOpening).Error; err != nil || persistedOpening.Account != intent.Account || !persistedOpening.Amount.Equal(initialBalance) {
			return errors.New("币安模拟账户初始余额与现有账户冲突")
		}
		var position tradingPosition
		positionErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account = ? AND mode = 'paper' AND market = ? AND instrument = ?", intent.Account, intent.Market, strings.ToUpper(intent.Instrument)).First(&position).Error
		if positionErr != nil && !errors.Is(positionErr, gorm.ErrRecordNotFound) {
			return errors.New("读取币安模拟持仓失败")
		}
		delta := quantity
		if intent.Side == "sell" {
			delta = delta.Neg()
		}
		nextQuantity := position.Quantity.Add(delta)
		if nextQuantity.Abs().Mul(quote.Price).GreaterThan(maxInstrument) {
			return errors.New("币安模拟持仓超过品种名义金额上限")
		}
		if intent.Market == "spot" && nextQuantity.Sign() < 0 {
			return errors.New("币安模拟持仓不足")
		}
		if intent.Market == "usdm" && intent.PositionEffect == "reduce" &&
			(position.Quantity.IsZero() || position.Quantity.Sign() == delta.Sign() || nextQuantity.Sign() != 0 && nextQuantity.Sign() != position.Quantity.Sign()) {
			return errors.New("币安模拟减仓订单会增加或反转持仓")
		}
		fee := notional.Mul(feeRate)
		var cash decimal.Decimal
		if err := tx.Model(&paperLedgerEntry{}).Where("account = ?", intent.Account).Select("COALESCE(SUM(amount), 0)").Scan(&cash).Error; err != nil {
			return errors.New("读取币安模拟现金余额失败")
		}
		if intent.Market == "spot" && intent.Side == "buy" && cash.LessThan(notional.Add(fee)) ||
			intent.Market == "usdm" && cash.LessThan(fee) {
			return errors.New("币安模拟现金余额不足")
		}
		now := time.Now().UTC()
		stored = tradingOrder{WorkflowID: workflowID, NodeInstanceID: request.NodeInstanceID, Account: intent.Account, Market: strings.ToLower(intent.Market), Instrument: strings.ToUpper(intent.Instrument), ClientOrderID: intent.ClientOrderID, Side: strings.ToLower(intent.Side), RequestQuantity: requestedQuantity, RequestQuoteAmount: requestedQuoteAmount, PositionEffect: intent.PositionEffect, Quantity: quantity, Executed: quantity, AveragePrice: quote.Price, Notional: notional, Status: "filled", Mode: "paper", OperationKey: request.OperationKey, CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "client_order_id"}}, DoNothing: true}).Create(&stored).Error; err != nil {
			return errors.New("保存币安模拟订单失败")
		}
		if stored.ID == 0 {
			if err := tx.Where("client_order_id = ?", intent.ClientOrderID).First(&stored).Error; err != nil {
				return err
			}
			if !matchesOrderIntent(stored, "paper", intent.Account, intent.Market, intent.Instrument, intent.Side, intent.PositionEffect, intent.ClientOrderID, requestedQuantity, requestedQuoteAmount) {
				return errors.New("币安客户订单编号已属于其他订单意图")
			}
			return nil
		}
		position.Account, position.Mode, position.Market, position.Instrument = intent.Account, "paper", strings.ToLower(intent.Market), strings.ToUpper(intent.Instrument)
		previousQuantity, previousAverage := position.Quantity, position.AveragePrice
		position.Quantity, position.AveragePrice = nextPaperPosition(previousQuantity, previousAverage, delta, quote.Price)
		position.UpdatedAt = now
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "account"}, {Name: "mode"}, {Name: "market"}, {Name: "instrument"}}, DoUpdates: clause.AssignmentColumns([]string{"quantity", "average_price", "updated_at"})}).Create(&position).Error; err != nil {
			return errors.New("保存币安模拟持仓失败")
		}
		fill := tradingFill{OrderID: stored.ID, ProviderTradeID: request.OperationKey, Quantity: quantity, Price: quote.Price, Fee: fee, FeeAsset: "QUOTE", FilledAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "provider_trade_id"}}, DoNothing: true}).Create(&fill).Error; err != nil {
			return errors.New("保存币安模拟成交失败")
		}
		if err := tx.Create(&tradingFee{FillID: fill.ID, Amount: fill.Fee, Asset: fill.FeeAsset, CreatedAt: now}).Error; err != nil {
			return errors.New("保存币安模拟手续费失败")
		}
		cashDelta := decimal.Zero
		if intent.Market == "spot" {
			cashDelta = notional.Neg()
			if intent.Side == "sell" {
				cashDelta = notional
			}
		} else if previousQuantity.Sign() != 0 && previousQuantity.Sign() != delta.Sign() {
			closed := decimal.Min(previousQuantity.Abs(), delta.Abs())
			cashDelta = quote.Price.Sub(previousAverage).Mul(closed)
			if previousQuantity.Sign() < 0 {
				cashDelta = cashDelta.Neg()
			}
		}
		if err := tx.Create(&paperLedgerEntry{Account: intent.Account, OperationKey: request.OperationKey, EntryType: "trade", Amount: cashDelta, OccurredAt: now}).Error; err != nil {
			return errors.New("保存币安模拟账本失败")
		}
		return tx.Create(&paperLedgerEntry{Account: intent.Account, OperationKey: request.OperationKey, EntryType: "fee", Amount: fee.Neg(), OccurredAt: now}).Error
	})
	if err != nil {
		return sdk.ActionResult{}, err
	}
	request.Logger.Info("Binance Paper order filled", "account", intent.Account, "market", intent.Market, "instrument", intent.Instrument, "client_order_id", intent.ClientOrderID)
	return sdk.ActionResult{Output: marshalOrder(stored)}, nil
}

func paperOpeningOperationKey(account string) string {
	digest := sha256.Sum256([]byte(account))
	return fmt.Sprintf("opening-%x", digest[:28])
}

var _ sdk.ActionHandler = paperExecuteAction{}

func (q *binanceRuntime) validateOrderRules(ctx context.Context, market, symbol string, quantity decimal.Decimal) error {
	if quantity.Sign() <= 0 {
		return nil
	}
	var instrument binanceInstrument
	if err := q.db.WithContext(ctx).Where("market = ? AND symbol = ?", market, strings.ToUpper(symbol)).First(&instrument).Error; err != nil {
		return errors.New("币安品种规则不可用")
	}
	if quantity.LessThan(instrument.MinQuantity) || !quantity.Mod(instrument.QuantityStep).IsZero() {
		return errors.New("币安订单数量不符合品种规则")
	}
	return nil
}

func (q *binanceRuntime) orderByClientID(ctx context.Context, id string) (tradingOrder, bool, error) {
	var row tradingOrder
	err := q.db.WithContext(ctx).Where("client_order_id = ?", id).First(&row).Error
	if err == nil {
		return row, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, false, nil
	}
	return row, false, errors.New("读取币安订单失败")
}

func matchesOrderIntent(row tradingOrder, mode, account, market, instrument, side, positionEffect, clientOrderID string, quantity, quoteAmount decimal.Decimal) bool {
	return row.Mode == mode && row.Account == account && row.Market == market && row.Instrument == instrument &&
		row.Side == side && row.PositionEffect == positionEffect && row.ClientOrderID == clientOrderID &&
		row.RequestQuantity.Equal(quantity) && row.RequestQuoteAmount.Equal(quoteAmount)
}

func marshalOrder(row tradingOrder) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"orderId": row.ID, "providerOrderId": row.ProviderOrderID, "clientOrderId": row.ClientOrderID, "status": row.Status, "market": row.Market, "instrument": row.Instrument, "side": row.Side, "quantity": row.Quantity.String(), "executed": row.Executed.String(), "averagePrice": row.AveragePrice.String(), "updatedAt": row.UpdatedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)})
	return raw
}

func zeroIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "0"
	}
	return value
}

func orderIntentSchema() json.RawMessage {
	return json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"venue":{"type":"string","const":"binance","title":"交易所"},"account":{"type":"string","pattern":"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$","title":"模拟账户"},"market":{"type":"string","enum":["spot","usdm"],"title":"市场","enumLabels":["现货","U 本位合约"]},"instrument":{"type":"string","pattern":"^[A-Z0-9]{2,32}$","title":"品种"},"side":{"type":"string","enum":["buy","sell"],"title":"方向","enumLabels":["买入","卖出"]},"quantity":{"type":"string","x-coinsphere-decimal":true,"title":"数量","description":"使用十进制字符串，避免浮点精度损失。"},"quoteAmount":{"type":"string","x-coinsphere-decimal":true,"title":"计价金额","description":"使用十进制字符串，避免浮点精度损失。"},"positionEffect":{"type":"string","enum":["open","reduce"],"title":"仓位操作","enumLabels":["开仓","减仓"]},"clientOrderId":{"type":"string","pattern":"^[A-Za-z0-9._:/-]{1,36}$","title":"客户端订单编号"},"referencePrice":{"type":"string","x-coinsphere-decimal":true,"title":"参考价格","description":"使用十进制字符串，避免浮点精度损失。"},"quotedAt":{"type":"string","format":"date-time","title":"报价时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"}},"required":["venue","account","market","instrument","side","quantity","quoteAmount","positionEffect","clientOrderId","referencePrice","quotedAt"],"additionalProperties":false}`)
}

func orderResultSchema() json.RawMessage {
	return json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"orderId":{"type":"integer","title":"订单编号"},"providerOrderId":{"type":"string","title":"渠道订单编号"},"clientOrderId":{"type":"string","title":"客户端订单编号"},"status":{"type":"string","title":"状态"},"market":{"type":"string","title":"市场"},"instrument":{"type":"string","title":"品种"},"side":{"type":"string","title":"方向"},"quantity":{"type":"string","x-coinsphere-decimal":true,"title":"数量","description":"使用十进制字符串，避免浮点精度损失。"},"executed":{"type":"string","x-coinsphere-decimal":true,"title":"已成交数量","description":"使用十进制字符串，避免浮点精度损失。"},"averagePrice":{"type":"string","x-coinsphere-decimal":true,"title":"平均价格","description":"使用十进制字符串，避免浮点精度损失。"},"updatedAt":{"type":"string","format":"date-time","title":"更新时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"}},"required":["orderId","providerOrderId","clientOrderId","status","market","instrument","side","quantity","executed","averagePrice","updatedAt"],"additionalProperties":false}`)
}

func nextPaperPosition(quantity, average, delta, price decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	next := quantity.Add(delta)
	if next.IsZero() {
		return decimal.Zero, decimal.Zero
	}
	if quantity.IsZero() || quantity.Sign() == delta.Sign() {
		return next, quantity.Abs().Mul(average).Add(delta.Abs().Mul(price)).Div(next.Abs())
	}
	if next.Sign() != quantity.Sign() {
		return next, price
	}
	return next, average
}
