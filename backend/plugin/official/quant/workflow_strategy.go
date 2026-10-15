package quant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"coinsphere/backend/plugin/sdk"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var quantCodeStrategyConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"series":{"type":"array","title":"行情序列","minItems":1,"maxItems":8,"items":{"type":"object","properties":{"alias":{"type":"string","title":"别名","pattern":"^[A-Za-z][A-Za-z0-9_]{0,63}$"},"venue":{"type":"string","title":"交易所","pattern":"^[a-z][a-z0-9_-]{1,31}$","default":"binance"},"market":{"type":"string","title":"市场","minLength":1,"maxLength":32},"instrument":{"type":"string","title":"品种","pattern":"^[A-Z0-9]{2,32}$"},"interval":{"type":"string","title":"周期","enum":["1m","3m","5m","15m","30m","1h","2h","4h","6h","8h","12h","1d","3d","1w"],"enumLabels":["1 分钟","3 分钟","5 分钟","15 分钟","30 分钟","1 小时","2 小时","4 小时","6 小时","8 小时","12 小时","1 天","3 天","1 周"]},"lookback":{"type":"integer","title":"回看根数","minimum":1,"maximum":500}},"required":["alias","venue","market","instrument","interval","lookback"],"additionalProperties":false},"default":[{"alias":"main","venue":"binance","market":"spot","instrument":"BTCUSDT","interval":"1h","lookback":30}]},"parameters":{"type":"object","title":"参数（小数使用字符串）","default":{"target":"1"}},"source":{"type":"string","title":"CEL 表达式","minLength":1,"maxLength":4096,"default":"{\"long\": decimalGt(last(ohlcv.main.close), sma(ohlcv.main.close, 20)), \"target\": params.target}"},"booleanOutputs":{"type":"array","title":"布尔输出","items":{"type":"string","pattern":"^[A-Za-z][A-Za-z0-9_]{0,63}$"},"minItems":1,"maxItems":32,"uniqueItems":true,"default":["long"]},"decimalOutputs":{"type":"array","title":"小数输出","items":{"type":"string","pattern":"^[A-Za-z][A-Za-z0-9_]{0,63}$"},"maxItems":32,"uniqueItems":true,"default":["target"]},"branchField":{"type":"string","title":"分支字段","pattern":"^[A-Za-z][A-Za-z0-9_]{0,63}$","default":"long"}},"required":["series","parameters","source","booleanOutputs","decimalOutputs","branchField"],"additionalProperties":false}`)

var quantCodeStrategyOutputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"booleans":{"type":"object","additionalProperties":{"type":"boolean"},"title":"布尔值"},"decimals":{"type":"object","additionalProperties":{"type":"string","pattern":"^-?[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true},"title":"十进制数值"},"ready":{"type":"boolean","title":"是否就绪"},"branch":{"type":"string","enum":["true","false"],"title":"分支","enumLabels":["是","否"]},"entered":{"type":"boolean","title":"是否进入"},"triggered":{"type":"boolean","title":"是否触发"},"evaluatedAt":{"type":"string","format":"date-time","title":"评估时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"}},"required":["booleans","decimals","ready","branch","entered","triggered","evaluatedAt"],"additionalProperties":false}`)

var quantPositionConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"market":{"type":"string","title":"市场","enum":["spot","usdm"],"default":"spot","enumLabels":["现货","U 本位合约"]},"targetMode":{"type":"string","title":"目标来源","enum":["fixed","input"],"enumLabels":["固定目标仓位","引用上游小数"],"default":"fixed"},"fixedTarget":{"type":"string","title":"固定目标仓位","pattern":"^-?[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"default":"1","description":"使用十进制字符串，避免浮点精度损失。"},"decimalField":{"type":"string","title":"小数字段名","pattern":"^[A-Za-z][A-Za-z0-9_]{0,63}$","default":"target"}},"required":["market","targetMode","fixedTarget","decimalField"],"additionalProperties":false}`)
var quantPositionInputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"target":{"type":"string","pattern":"^-?[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"x-coinsphere-field-source":true,"title":"目标仓位","description":"使用十进制字符串，避免浮点精度损失。"},"evaluatedAt":{"type":"string","format":"date-time","x-coinsphere-field-source":true,"title":"评估时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"}},"required":["evaluatedAt"],"additionalProperties":false}`)
var quantPositionOutputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"targetPosition":{"type":"string","x-coinsphere-decimal":true,"title":"目标仓位","description":"使用十进制字符串，避免浮点精度损失。"},"evaluatedAt":{"type":"string","format":"date-time","title":"评估时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"},"sourceNodeInstanceId":{"type":"string","title":"来源节点标识"}},"required":["targetPosition","evaluatedAt","sourceNodeInstanceId"],"additionalProperties":false}`)

var quantOutputSignalInputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false}`)
var quantOutputSignalOutputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"signalId":{"type":"integer","minimum":0,"title":"信号编号"},"businessKey":{"type":"string","title":"业务标识"},"action":{"type":"string","enum":["buy","sell","hold"],"title":"操作","enumLabels":["买入","卖出","观望"]},"previousTargetPosition":{"type":"string","x-coinsphere-decimal":true,"title":"上次目标仓位","description":"使用十进制字符串，避免浮点精度损失。"},"targetPosition":{"type":"string","x-coinsphere-decimal":true,"title":"目标仓位","description":"使用十进制字符串，避免浮点精度损失。"},"target":{"type":"string","x-coinsphere-decimal":true,"title":"目标仓位","description":"使用十进制字符串，避免浮点精度损失。"},"evaluatedAt":{"type":"string","format":"date-time","title":"评估时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"},"nodeValues":{"type":"object","title":"节点数值"},"branch":{"type":"string","enum":["realtime","unchanged"],"title":"分支","enumLabels":["实时","保持原值"]}},"required":["signalId","businessKey","action","previousTargetPosition","targetPosition","target","evaluatedAt","nodeValues","branch"],"additionalProperties":false}`)

var quantBacktestStartConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"venue":{"type":"string","title":"交易所","pattern":"^[a-z][a-z0-9_-]{1,31}$","default":"binance"},"market":{"type":"string","title":"市场","minLength":1,"maxLength":32},"instrument":{"type":"string","title":"主品种","pattern":"^[A-Z0-9]{2,32}$","default":"BTCUSDT"},"interval":{"type":"string","title":"主周期","enum":["1m","3m","5m","15m","30m","1h","2h","4h","6h","8h","12h","1d","3d","1w"],"default":"1h","enumLabels":["1 分钟","3 分钟","5 分钟","15 分钟","30 分钟","1 小时","2 小时","4 小时","6 小时","8 小时","12 小时","1 天","3 天","1 周"]}},"required":["venue","market","instrument","interval"],"additionalProperties":false}`)
var quantBacktestStartInputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"startTime":{"type":"string","format":"date-time","title":"开始时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"},"endTime":{"type":"string","format":"date-time","title":"结束时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"},"initialCapital":{"type":"string","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"title":"初始资金","description":"使用十进制字符串，避免浮点精度损失。"},"feeRate":{"type":"string","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"title":"手续费率","description":"使用十进制字符串，避免浮点精度损失。"},"slippageRate":{"type":"string","pattern":"^[0-9]+(?:\\.[0-9]+)?$","x-coinsphere-decimal":true,"title":"滑点率","description":"使用十进制字符串，避免浮点精度损失。"}},"required":["startTime","endTime","initialCapital","feeRate","slippageRate"],"additionalProperties":false}`)
var quantBacktestStartOutputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"branch":{"type":"string","enum":["each","completed"],"title":"分支","enumLabels":["每次循环","循环完成"]},"backtestId":{"type":"integer","title":"回测编号"},"venue":{"type":"string","title":"交易所"},"strategyId":{"type":"string","title":"策略标识"},"strategyVersion":{"type":"string","title":"策略版本"},"finalEquity":{"type":"string","x-coinsphere-decimal":true,"title":"最终权益","description":"使用十进制字符串，避免浮点精度损失。"},"totalReturn":{"type":"string","x-coinsphere-decimal":true,"title":"总收益率","description":"使用十进制字符串，避免浮点精度损失。"},"maxDrawdown":{"type":"string","x-coinsphere-decimal":true,"description":"使用十进制字符串，避免浮点精度损失。","title":"最大回撤"},"totalFees":{"type":"string","x-coinsphere-decimal":true,"title":"手续费合计","description":"使用十进制字符串，避免浮点精度损失。"},"tradeCount":{"type":"integer","title":"成交次数"},"candleCount":{"type":"integer","title":"K 线数量"}},"required":["branch","backtestId","venue","strategyId","strategyVersion","finalEquity","totalReturn","maxDrawdown","totalFees","tradeCount","candleCount"],"additionalProperties":false}`)

type quantPositionAction struct{}
type quantOutputSignalAction struct{ runtime *quantRuntime }

func validateQuantPositionConfig(raw json.RawMessage) error {
	var config struct {
		Market, TargetMode, FixedTarget, DecimalField string
	}
	if !decodeQuantStrict(raw, &config) || !quantCodeNamePattern.MatchString(config.DecimalField) ||
		config.TargetMode != "fixed" && config.TargetMode != "input" {
		return errors.New("量化仓位配置无效")
	}
	target, err := decimal.NewFromString(config.FixedTarget)
	if err != nil || config.Market != "spot" && config.Market != "usdm" || config.TargetMode == "fixed" &&
		(config.Market == "spot" && (target.Sign() < 0 || target.GreaterThan(quantOne)) ||
			config.Market == "usdm" && (target.LessThan(decimal.NewFromInt(-1)) || target.GreaterThan(quantOne))) {
		return errors.New("量化目标仓位超出市场允许范围")
	}
	return nil
}

func (q *quantRuntime) registerWorkflowStrategyNodes(registrar sdk.Registrar) error {
	nodes := []struct {
		descriptor  sdk.NodeDescriptor
		handler     sdk.ActionHandler
		title       string
		description string
		category    string
		color       string
		icon        string
	}{
		{sdk.NodeDescriptor{
			Type: "official.quant.backtest_start", Version: "1.0.0", Kind: sdk.NodeKindAction,
			Branches: []string{"each", "completed"}, ConfigSchema: quantBacktestStartConfigSchema,
			UISchema:    json.RawMessage(`{"ui:order":["venue","market","instrument","interval"]}`),
			InputSchema: quantBacktestStartInputSchema, OutputSchema: quantBacktestStartOutputSchema,
			Pool: sdk.PoolCompute, SideEffect: sdk.SideEffectData, RetrySafe: true, State: sdk.StateStateless,
			Capabilities: sdk.NodeCapabilities{Deterministic: false, Stateless: true},
		}, quantWorkflowBacktestAction{runtime: q}, "回测开始", "逐帧执行通用量化工作流并汇总回测结果。", "start", "#7c3aed", "history"},
		{sdk.NodeDescriptor{
			Type: "official.quant.code_strategy", Version: "1.0.0", Kind: sdk.NodeKindAction,
			Branches: []string{"true", "false"}, ConfigSchema: quantCodeStrategyConfigSchema,
			UISchema:     json.RawMessage(`{"ui:order":["series","parameters","source","booleanOutputs","decimalOutputs","branchField"],"source":{"ui:widget":"code","ui:language":"cel"}}`),
			InputSchema:  json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"eventTime":{"type":"string","format":"date-time","title":"事件时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"},"pathEntered":{"type":"boolean","title":"是否进入路径"}},"required":["eventTime"],"additionalProperties":false}`),
			OutputSchema: quantCodeStrategyOutputSchema, Pool: sdk.PoolCompute, SideEffect: sdk.SideEffectNone, State: sdk.StateStateless,
			Capabilities:   sdk.NodeCapabilities{Deterministic: true, Stateless: true},
			ValidateConfig: validateQuantCodeStrategyConfig,
		}, &quantCodeStrategyAction{runtime: q}, "代码策略", "使用受限 CEL 对多行情序列执行确定性策略判断。", "strategy", "#2563eb", "code-xml"},
		{sdk.NodeDescriptor{
			Type: "official.quant.position", Version: "1.0.0", Kind: sdk.NodeKindAction,
			ConfigSchema: quantPositionConfigSchema, UISchema: json.RawMessage(`{"ui:order":["market","targetMode","fixedTarget"]}`),
			InputSchema: quantPositionInputSchema, OutputSchema: quantPositionOutputSchema,
			Pool: sdk.PoolCompute, SideEffect: sdk.SideEffectNone, State: sdk.StateStateless,
			Capabilities:   sdk.NodeCapabilities{Deterministic: true, Stateless: true},
			ValidateConfig: validateQuantPositionConfig,
		}, quantPositionAction{}, "仓位计算", "将策略输出转换为通用目标仓位。", "strategy", "#2563eb", "percent"},
		{sdk.NodeDescriptor{
			Type: "official.quant.output_signal", Version: "1.0.0", Kind: sdk.NodeKindAction,
			Branches: []string{"realtime", "unchanged"}, ConfigSchema: quantSeriesConfigSchema,
			UISchema:    json.RawMessage(`{"ui:order":["venue","market","instrument","interval"]}`),
			InputSchema: quantOutputSignalInputSchema, OutputSchema: quantOutputSignalOutputSchema,
			Pool: sdk.PoolStream, SideEffect: sdk.SideEffectData, State: sdk.StateStateless,
			Capabilities: sdk.NodeCapabilities{Deterministic: true, Stateless: true},
		}, quantOutputSignalAction{runtime: q}, "输出策略信号", "汇总目标仓位并持久化通用量化 Signal。", "strategy", "#0f766e", "radio"},
	}
	for _, node := range nodes {
		if err := registrar.Action(quantNodeMeta(node.descriptor, node.title, node.description, node.category, node.color, node.icon), node.handler); err != nil {
			return err
		}
	}
	return nil
}

func (quantPositionAction) Execute(_ context.Context, request sdk.ActionRequest) (sdk.ActionResult, error) {
	var config struct {
		Market, TargetMode, FixedTarget, DecimalField string
	}
	var input struct {
		Target, EvaluatedAt string
	}
	if !decodeQuantStrict(request.Config, &config) || !decodeQuantStrict(request.Input, &input) {
		return sdk.ActionResult{}, errors.New("量化仓位配置或输入无效")
	}
	targetText := config.FixedTarget
	if config.TargetMode == "input" {
		targetText = input.Target
	}
	target, err := decimal.NewFromString(targetText)
	if err != nil || config.TargetMode != "fixed" && config.TargetMode != "input" ||
		config.Market == "spot" && (target.Sign() < 0 || target.GreaterThan(quantOne)) ||
		config.Market == "usdm" && (target.LessThan(decimal.NewFromInt(-1)) || target.GreaterThan(quantOne)) {
		return sdk.ActionResult{}, errors.New("量化目标仓位超出市场允许范围")
	}
	evaluatedAt, err := parseQuantUTCTime(input.EvaluatedAt)
	if err != nil {
		return sdk.ActionResult{}, err
	}
	return sdk.ActionResult{Output: mustMarshal(map[string]any{
		"targetPosition": target.String(), "evaluatedAt": evaluatedAt.Format(time.RFC3339Nano),
		"sourceNodeInstanceId": request.NodeInstanceID,
	})}, nil
}

func (a quantOutputSignalAction) Execute(ctx context.Context, request sdk.ActionRequest) (sdk.ActionResult, error) {
	config, err := parseQuantSeriesConfig(request.Config)
	if err != nil {
		return sdk.ActionResult{}, err
	}
	var input struct {
		Candidates []struct {
			TargetPosition, EvaluatedAt, SourceNodeInstanceID string
		} `json:"candidates"`
		PreviousTargetPosition string         `json:"previousTargetPosition"`
		NodeValues             map[string]any `json:"nodeValues"`
	}
	for _, incoming := range request.Incoming {
		var candidate struct{ TargetPosition, EvaluatedAt, SourceNodeInstanceID string }
		if json.Unmarshal(incoming.Output, &candidate) == nil && candidate.TargetPosition != "" {
			if candidate.SourceNodeInstanceID == "" {
				candidate.SourceNodeInstanceID = incoming.NodeInstanceID
			}
			input.Candidates = append(input.Candidates, candidate)
		}
	}
	if len(quantFrameData(ctx)) > 0 {
		var frame struct {
			PreviousTargetPosition string `json:"previousTargetPosition"`
		}
		if json.Unmarshal(quantFrameData(ctx), &frame) != nil {
			return sdk.ActionResult{}, errors.New("量化信号输出的行情帧上下文无效")
		}
		input.PreviousTargetPosition = frame.PreviousTargetPosition
	}
	if len(input.Candidates) == 0 {
		return sdk.ActionResult{}, errors.New("量化信号输出需要至少一个已到达的仓位节点")
	}
	target, err := decimal.NewFromString(input.Candidates[0].TargetPosition)
	if err != nil {
		return sdk.ActionResult{}, errors.New("量化信号输出的目标仓位无效")
	}
	evaluatedAt, err := parseQuantUTCTime(input.Candidates[0].EvaluatedAt)
	if err != nil {
		return sdk.ActionResult{}, err
	}
	nodeValues := input.NodeValues
	if nodeValues == nil {
		nodeValues = map[string]any{}
	}
	for _, candidate := range input.Candidates {
		value, valueErr := decimal.NewFromString(candidate.TargetPosition)
		at, timeErr := parseQuantUTCTime(candidate.EvaluatedAt)
		if valueErr != nil || timeErr != nil || !value.Equal(target) || !at.Equal(evaluatedAt) {
			return sdk.ActionResult{}, fmt.Errorf("量化仓位在 %s 发生冲突", evaluatedAt.Format(time.RFC3339Nano))
		}
		nodeValues[candidate.SourceNodeInstanceID] = map[string]any{"targetPosition": value.String()}
	}
	workflowID, err := quantInt64(request.Revision.WorkflowID)
	if err != nil {
		return sdk.ActionResult{}, errors.New("量化工作流标识无效")
	}
	businessKey := fmt.Sprintf("quant:%s:%s:%s", config.Market, config.Instrument, request.NodeInstanceID)
	previous := decimal.Zero
	if isQuantFrame(ctx) {
		if input.PreviousTargetPosition != "" {
			previous, err = decimal.NewFromString(input.PreviousTargetPosition)
		}
	} else {
		previous, err = a.runtime.latestQuantWorkflowTarget(ctx, workflowID, request.NodeInstanceID, businessKey, request.OperationKey)
	}
	if err != nil {
		return sdk.ActionResult{}, err
	}
	action, branch := "hold", "unchanged"
	if target.GreaterThan(previous) {
		action = "buy"
	} else if target.LessThan(previous) {
		action = "sell"
	}
	signalID := int64(0)
	if !target.Equal(previous) {
		branch = "realtime"
		if !isQuantFrame(ctx) {
			result, err := (quantSignalAction{runtime: a.runtime}).Execute(ctx, sdk.ActionRequest{
				Revision: request.Revision, NodeInstanceID: request.NodeInstanceID, OperationKey: request.OperationKey,
				Config: request.Config,
				Input: mustMarshal(map[string]any{"strategyId": "workflow", "strategyVersion": request.Revision.RevisionID,
					"target": target.String(), "evaluatedAt": evaluatedAt.Format(time.RFC3339Nano), "businessKey": businessKey}),
			})
			if err != nil {
				return sdk.ActionResult{}, err
			}
			var persisted struct {
				SignalID int64 `json:"signalId"`
			}
			_ = json.Unmarshal(result.Output, &persisted)
			signalID = persisted.SignalID
		}
	}
	return sdk.ActionResult{Output: mustMarshal(map[string]any{
		"signalId": signalID, "businessKey": businessKey, "action": action,
		"previousTargetPosition": previous.String(), "targetPosition": target.String(), "target": target.String(),
		"evaluatedAt": evaluatedAt.Format(time.RFC3339Nano), "nodeValues": nodeValues, "branch": branch,
	})}, nil
}

func (q *quantRuntime) latestQuantWorkflowTarget(ctx context.Context, workflowID int64, nodeID, businessKey, excludedOperation string) (decimal.Decimal, error) {
	var signal quantSignal
	err := q.db.WithContext(ctx).Where(
		"workflow_id = ? AND node_instance_id = ? AND business_key = ? AND operation_key <> ?",
		workflowID, nodeID, businessKey, excludedOperation,
	).Order("evaluated_at DESC, id DESC").First(&signal).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return decimal.Zero, nil
	}
	if err != nil {
		return decimal.Zero, errors.New("读取上次量化工作流目标失败")
	}
	return signal.Target, nil
}

var _ sdk.ActionHandler = quantPositionAction{}
var _ sdk.ActionHandler = quantOutputSignalAction{}
