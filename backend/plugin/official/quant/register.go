package quant

import (
	"encoding/json"

	"coinsphere/backend/plugin/contracts/trading"
	"coinsphere/backend/plugin/sdk"
	"gorm.io/gorm"
)

const quantPluginID = "official.quant"

var emptyObjectSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false}`)
var quantSeriesConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"venue":{"type":"string","title":"交易所","pattern":"^[a-z][a-z0-9_-]{1,31}$","default":"binance"},"market":{"type":"string","title":"市场类型","minLength":1,"maxLength":32},"instrument":{"type":"string","title":"交易对","pattern":"^[A-Z0-9]{2,32}$"},"interval":{"type":"string","title":"K 线周期","enum":["1m","3m","5m","15m","30m","1h","2h","4h","6h","8h","12h","1d","3d","1w"],"enumLabels":["1 分钟","3 分钟","5 分钟","15 分钟","30 分钟","1 小时","2 小时","4 小时","6 小时","8 小时","12 小时","1 天","3 天","1 周"]}},"required":["venue","market","instrument","interval"],"additionalProperties":false}`)
var quantStrategyConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"venue":{"type":"string","title":"交易所","pattern":"^[a-z][a-z0-9_-]{1,31}$","default":"binance"},"strategyId":{"type":"string","title":"策略标识","const":"official.quant.sma-crossover"},"market":{"type":"string","title":"市场类型","minLength":1,"maxLength":32},"instrument":{"type":"string","title":"交易对","pattern":"^[A-Z0-9]{2,32}$"},"interval":{"type":"string","title":"K 线周期","enum":["1m","3m","5m","15m","30m","1h","2h","4h","6h","8h","12h","1d","3d","1w"],"enumLabels":["1 分钟","3 分钟","5 分钟","15 分钟","30 分钟","1 小时","2 小时","4 小时","6 小时","8 小时","12 小时","1 天","3 天","1 周"]},"parameters":{"type":"object","title":"参数"}},"required":["venue","strategyId","market","instrument","interval","parameters"],"additionalProperties":false}`)
var quantBacktestConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"venue":{"type":"string","title":"交易所","pattern":"^[a-z][a-z0-9_-]{1,31}$","default":"binance"},"strategyId":{"type":"string","title":"策略标识","const":"official.quant.sma-crossover"},"market":{"type":"string","title":"市场类型","minLength":1,"maxLength":32},"instrument":{"type":"string","title":"交易对","pattern":"^[A-Z0-9]{2,32}$"},"interval":{"type":"string","title":"K 线周期","enum":["1m","3m","5m","15m","30m","1h","2h","4h","6h","8h","12h","1d","3d","1w"],"enumLabels":["1 分钟","3 分钟","5 分钟","15 分钟","30 分钟","1 小时","2 小时","4 小时","6 小时","8 小时","12 小时","1 天","3 天","1 周"]},"startTime":{"type":"string","title":"开始时间（UTC）","format":"date-time","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"},"endTime":{"type":"string","title":"结束时间（UTC）","format":"date-time","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"},"initialCapital":{"type":"string","title":"初始资金","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"description":"使用十进制字符串，避免浮点精度损失。"},"feeRate":{"type":"string","title":"手续费率","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"description":"使用十进制字符串，避免浮点精度损失。"},"slippageRate":{"type":"string","title":"滑点率","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"description":"使用十进制字符串，避免浮点精度损失。"},"parameters":{"type":"object","title":"参数"}},"required":["venue","strategyId","market","instrument","interval","startTime","endTime","initialCapital","feeRate","slippageRate","parameters"],"additionalProperties":false}`)

type quantRuntime struct {
	financial        *trading.Registry
	frameActions     map[string]sdk.ActionHandler
	frameDescriptors map[string]sdk.NodeDescriptor
	db               *gorm.DB
	registry         trading.StrategyRegistry
	marketData       trading.MarketDataRegistry
}

func Register(registrar sdk.Registrar, host sdk.Host, financial *trading.Registry) error {
	runtime := &quantRuntime{
		db: host.Store.DB(), registry: financial, marketData: financial, financial: financial,
	}
	return runtime.register(quantRegistrar{Registrar: registrar, runtime: runtime})
}

func (q *quantRuntime) register(registrar sdk.Registrar) error {
	if err := registrar.Cleanup(cleanupWorkflow); err != nil {
		return err
	}
	if err := registrar.WorkflowValidator(sdk.WorkflowValidatorFunc(validateQuantWorkflow)); err != nil {
		return err
	}
	if err := registerTemplates(registrar); err != nil {
		return err
	}
	if err := q.financial.RegisterStrategy(smaCrossoverStrategy{}); err != nil {
		return err
	}
	if err := registrar.Action(quantNodeMeta(sdk.NodeDescriptor{
		Type: "official.quant.evaluate", Version: "1.0.0", Kind: sdk.NodeKindAction,
		ConfigSchema: quantStrategyConfigSchema,
		UISchema:     json.RawMessage(`{"ui:order":["venue","strategyId","market","instrument","interval","parameters"]}`),
		InputSchema:  json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"eventTime":{"type":"string","format":"date-time","title":"事件时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"}},"required":["eventTime"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"venue":{"type":"string","title":"交易所"},"strategyId":{"type":"string","title":"策略标识"},"strategyVersion":{"type":"string","title":"策略版本"},"target":{"type":"string","pattern":"^-?[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"title":"目标仓位","description":"使用十进制字符串，避免浮点精度损失。"},"evaluatedAt":{"type":"string","format":"date-time","title":"评估时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"}},"required":["venue","strategyId","strategyVersion","target","evaluatedAt"],"additionalProperties":false}`),
		Pool:         sdk.PoolCompute, SideEffect: sdk.SideEffectNone, State: sdk.StateStateless,
		Capabilities: sdk.NodeCapabilities{Deterministic: true, Stateless: true},
	}, "量化策略评估", "使用行情 Provider 运行通用量化策略。", "strategy", "#2563eb", "chart-no-axes-combined"), quantEvaluateAction{runtime: q}); err != nil {
		return err
	}
	for _, indicator := range quantIndicatorDefinitions {
		if err := registrar.Action(quantNodeMeta(sdk.NodeDescriptor{
			Type: indicator.NodeType, Version: "1.0.0", Kind: sdk.NodeKindAction,
			Branches: []string{"true", "false"}, ConfigSchema: quantIndicatorConfigSchema(indicator.Indicator),
			UISchema:    json.RawMessage(`{"ui:order":["venue","market","instrument","checkInterval","name","interval","parameters"]}`),
			InputSchema: quantIndicatorInputSchema, OutputSchema: quantIndicatorOutputSchema,
			Pool: sdk.PoolCompute, SideEffect: sdk.SideEffectNone, State: sdk.StateStateless,
			Capabilities: sdk.NodeCapabilities{Deterministic: true, Stateless: true},
		}, indicator.Title, "基于闭合 K 线确定性计算 "+indicator.Title+"。", "market", "#0f766e", indicator.Icon), quantIndicatorAction{runtime: q, indicator: indicator.Indicator}); err != nil {
			return err
		}
	}
	if err := q.registerWorkflowStrategyNodes(registrar); err != nil {
		return err
	}
	if err := q.registerMarketSignals(registrar); err != nil {
		return err
	}
	if err := q.registerOrderIntent(registrar); err != nil {
		return err
	}
	if err := registrar.Action(quantNodeMeta(sdk.NodeDescriptor{
		Type: "official.quant.backtest", Version: "1.0.0", Kind: sdk.NodeKindAction,
		ConfigSchema: quantBacktestConfigSchema,
		UISchema:     json.RawMessage(`{"ui:order":["venue","strategyId","market","instrument","interval","startTime","endTime","initialCapital","feeRate","slippageRate","parameters"]}`),
		InputSchema:  emptyObjectSchema,
		OutputSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"backtestId":{"type":"integer","title":"回测编号"},"venue":{"type":"string","title":"交易所"},"strategyId":{"type":"string","title":"策略标识"},"strategyVersion":{"type":"string","title":"策略版本"},"finalEquity":{"type":"string","x-coinsphere-decimal":true,"title":"最终权益","description":"使用十进制字符串，避免浮点精度损失。"},"totalReturn":{"type":"string","x-coinsphere-decimal":true,"title":"总收益率","description":"使用十进制字符串，避免浮点精度损失。"},"maxDrawdown":{"type":"string","x-coinsphere-decimal":true,"description":"使用十进制字符串，避免浮点精度损失。","title":"最大回撤"},"totalFees":{"type":"string","x-coinsphere-decimal":true,"title":"手续费合计","description":"使用十进制字符串，避免浮点精度损失。"},"tradeCount":{"type":"integer","title":"成交次数"},"candleCount":{"type":"integer","title":"K 线数量"}},"required":["backtestId","venue","strategyId","strategyVersion","finalEquity","totalReturn","maxDrawdown","totalFees","tradeCount","candleCount"],"additionalProperties":false}`),
		Pool:         sdk.PoolCompute, SideEffect: sdk.SideEffectData, RetrySafe: true, State: sdk.StateStateless,
	}, "量化策略回测", "通过任意行情 Provider 执行确定性回测。", "strategy", "#7c3aed", "history"), quantBacktestAction{runtime: q}); err != nil {
		return err
	}
	if err := registrar.ResultPage(sdk.ResultPageDescriptor{PageKey: "backtests", Title: "回测结果", ComponentEntry: "./official/quant/BacktestResults.vue", ScopeSchema: quantResultScopeSchema, FilterSchema: quantResultFilterSchema, Actions: []string{}, Resources: quantResultResources, ValidateScope: validateQuantResultScope}); err != nil {
		return err
	}
	for _, route := range []struct {
		desc    sdk.RouteDescriptor
		handler sdk.ScopedRouteHandler
	}{
		{sdk.RouteDescriptor{ResultPageKey: "backtests", Method: "GET", Pattern: "/backtests", Scope: sdk.ScopeResult}, q.handleQuantBacktests},
		{sdk.RouteDescriptor{ResultPageKey: "backtests", Method: "GET", Pattern: "/backtests/:backtestId", Scope: sdk.ScopeResult}, q.handleQuantBacktest},
		{sdk.RouteDescriptor{Method: "GET", Pattern: "/strategies", Scope: sdk.ScopeSystem}, q.handleQuantStrategies},
		{sdk.RouteDescriptor{Method: "GET", Pattern: "/backtests", Scope: sdk.ScopeSystem}, q.handleQuantBacktests},
		{sdk.RouteDescriptor{Method: "GET", Pattern: "/backtests/:backtestId", Scope: sdk.ScopeSystem}, q.handleQuantBacktest},
		{sdk.RouteDescriptor{Method: "GET", Pattern: "/market-signals", Scope: sdk.ScopeSystem}, q.handleQuantMarketSignals},
		{sdk.RouteDescriptor{Method: "GET", Pattern: "/signals", Scope: sdk.ScopeSystem}, q.handleQuantSignals},
	} {
		if err := registrar.Route(route.desc, route.handler); err != nil {
			return err
		}
	}
	if err := registrar.AssistantQuery(sdk.AssistantQueryDescriptor{
		Name: "query", Description: "查询 Quant 回测与信号的有界摘要。",
		InputSchema: quantAssistantQuerySchema,
	}, sdk.AssistantQueryHandlerFunc(q.assistantQuery)); err != nil {
		return err
	}
	if err := registrar.RunPanel(sdk.RunPanelDescriptor{PanelKey: "quant", Title: "量化结果", ComponentEntry: "./official/quant/ResultPage.vue", NodeTypes: []string{"official.quant.backtest", "official.quant.evaluate"}}); err != nil {
		return err
	}
	return registrar.RunPanel(sdk.RunPanelDescriptor{PanelKey: "analysis", Title: "回测分析", ComponentEntry: "./official/quant/BacktestAnalysis.vue", NodeTypes: []string{"official.quant.backtest"}})
}
