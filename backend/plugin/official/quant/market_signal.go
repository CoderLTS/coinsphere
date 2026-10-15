package quant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"coinsphere/backend/plugin/sdk"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var quantMarketSignalInputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"venue":{"type":"string","title":"交易所","pattern":"^[a-z][a-z0-9_-]{1,31}$","x-coinsphere-field-source":true},"market":{"type":"string","title":"市场类型","enum":["spot","usdm"],"x-coinsphere-field-source":true,"enumLabels":["现货","U 本位合约"]},"instrument":{"type":"string","title":"交易对","pattern":"^[A-Z0-9]{2,32}$","x-coinsphere-field-source":true},"interval":{"type":"string","title":"K 线周期","enum":["1m","3m","5m","15m","30m","1h","2h","4h","6h","8h","12h","1d","3d","1w"],"x-coinsphere-field-source":true,"enumLabels":["1 分钟","3 分钟","5 分钟","15 分钟","30 分钟","1 小时","2 小时","4 小时","6 小时","8 小时","12 小时","1 天","3 天","1 周"]},"name":{"type":"string","title":"信号名称","minLength":1,"maxLength":80,"x-coinsphere-field-source":true},"indicator":{"type":"string","title":"指标","minLength":1,"maxLength":64,"x-coinsphere-field-source":true},"candleCloseTime":{"type":"string","title":"K 线收盘时间","format":"date-time","x-coinsphere-field-source":true,"description":"使用 UTC 时间，接口采用 ISO 8601 格式。"},"summary":{"type":"string","title":"摘要","minLength":1,"maxLength":2000,"x-coinsphere-field-source":true},"values":{"type":"object","title":"数值","additionalProperties":{"type":"string"},"x-coinsphere-field-source":true}},"required":["venue","market","instrument","interval","name","indicator","candleCloseTime","summary","values"],"additionalProperties":false}`)

var quantMarketSignalOutputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"signalId":{"type":"integer","minimum":1,"title":"信号编号"},"venue":{"type":"string","title":"交易所"},"market":{"type":"string","title":"市场"},"instrument":{"type":"string","title":"品种"},"interval":{"type":"string","title":"K 线周期"},"name":{"type":"string","title":"名称"},"indicator":{"type":"string","title":"指标"},"candleCloseTime":{"type":"string","format":"date-time","title":"K 线收盘时间","description":"使用 UTC 时间，接口采用 ISO 8601 格式。"}},"required":["signalId","venue","market","instrument","interval","name","indicator","candleCloseTime"],"additionalProperties":false}`)

type quantMarketSignalAction struct{ runtime *quantRuntime }

func (q *quantRuntime) registerMarketSignals(registrar sdk.Registrar) error {
	return registrar.Action(quantNodeMeta(sdk.NodeDescriptor{
		Type: "official.quant.market_signal", Version: "1.0.0", Kind: sdk.NodeKindAction,
		ConfigSchema: emptyObjectSchema, UISchema: json.RawMessage(`{"ui:order":[]}`),
		InputSchema: quantMarketSignalInputSchema, OutputSchema: quantMarketSignalOutputSchema,
		Pool: sdk.PoolStream, SideEffect: sdk.SideEffectData, RetrySafe: true, State: sdk.StateStateless,
	}, "输出信号", "持久化通用指标判断产生的行情 Signal。", "market", "#0f766e", "radio-tower"), quantMarketSignalAction{runtime: q})
}

func (a quantMarketSignalAction) Execute(ctx context.Context, request sdk.ActionRequest) (sdk.ActionResult, error) {
	var input struct {
		Venue, Market, Instrument, Interval, Name, Indicator, CandleCloseTime, Summary string
		Values                                                                         map[string]string `json:"values"`
	}
	if !decodeQuantStrict(request.Input, &input) {
		return sdk.ActionResult{}, errors.New("量化行情信号输入无效")
	}
	series, err := parseQuantSeriesConfig(mustMarshal(map[string]any{
		"venue": input.Venue, "market": input.Market, "instrument": input.Instrument, "interval": input.Interval,
	}))
	if err != nil {
		return sdk.ActionResult{}, err
	}
	input.Name, input.Indicator, input.Summary = strings.TrimSpace(input.Name), strings.TrimSpace(input.Indicator), strings.TrimSpace(input.Summary)
	candleCloseTime, timeErr := parseQuantUTCTime(input.CandleCloseTime)
	workflowID, workflowErr := quantInt64(request.Revision.WorkflowID)
	revisionID, revisionErr := quantInt64(request.Revision.RevisionID)
	if timeErr != nil || workflowErr != nil || revisionErr != nil || input.Values == nil ||
		!quantMarketSignalIndicator(input.Indicator) || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 80 ||
		utf8.RuneCountInString(input.Summary) < 1 || utf8.RuneCountInString(input.Summary) > 2000 {
		return sdk.ActionResult{}, errors.New("量化行情信号标识无效")
	}
	if existing, ok, err := a.runtime.loadQuantMarketSignalByOperation(ctx, request.OperationKey); err != nil {
		return sdk.ActionResult{}, err
	} else if ok {
		return quantMarketSignalResult(existing), nil
	}
	values, err := json.Marshal(input.Values)
	if err != nil {
		return sdk.ActionResult{}, errors.New("编码量化行情信号数值失败")
	}
	row := quantMarketSignal{
		OperationKey: request.OperationKey, WorkflowID: workflowID, RevisionID: revisionID,
		NodeInstanceID: request.NodeInstanceID, Market: series.Market, Instrument: series.Instrument,
		Venue:    series.Venue,
		Interval: series.Interval, Name: input.Name, Indicator: input.Indicator,
		CandleCloseTime: candleCloseTime, Summary: input.Summary, Values: string(values), CreatedAt: time.Now().UTC(),
	}
	if err := a.runtime.db.WithContext(ctx).Create(&row).Error; err != nil {
		if existing, ok, loadErr := a.runtime.loadQuantMarketSignalByOperation(ctx, request.OperationKey); loadErr == nil && ok {
			return quantMarketSignalResult(existing), nil
		}
		return sdk.ActionResult{}, errors.New("保存量化行情信号失败")
	}
	return quantMarketSignalResult(row), nil
}

func quantMarketSignalIndicator(value string) bool {
	for _, definition := range quantIndicatorDefinitions {
		if definition.Indicator == value {
			return true
		}
	}
	return false
}

func (q *quantRuntime) loadQuantMarketSignalByOperation(ctx context.Context, operationKey string) (quantMarketSignal, bool, error) {
	var signal quantMarketSignal
	if err := q.db.WithContext(ctx).Where("operation_key = ?", operationKey).First(&signal).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return quantMarketSignal{}, false, nil
		}
		return quantMarketSignal{}, false, errors.New("读取量化行情信号失败")
	}
	return signal, true, nil
}

func quantMarketSignalResult(signal quantMarketSignal) sdk.ActionResult {
	return sdk.ActionResult{Output: mustMarshal(map[string]any{
		"signalId": signal.ID, "venue": signal.Venue, "market": signal.Market, "instrument": signal.Instrument,
		"interval": signal.Interval, "name": signal.Name, "indicator": signal.Indicator,
		"candleCloseTime": signal.CandleCloseTime.UTC().Format(time.RFC3339Nano),
	})}
}

func (q *quantRuntime) handleQuantMarketSignals(c *gin.Context, scope sdk.RouteScope) {
	if !validQuantSystemScope(scope) || !quantQueryKeys(c.Request, "venue", "market", "instrument", "interval", "startTime", "endTime", "limit") {
		writeQuantProblem(c, http.StatusBadRequest, "invalid Quant market signal query")
		return
	}
	series, err := parseQuantSeriesConfig(mustMarshal(map[string]any{
		"venue": c.Query("venue"), "market": c.Query("market"), "instrument": c.Query("instrument"), "interval": c.Query("interval"),
	}))
	if err != nil {
		writeQuantProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := quantQueryLimit(c.Request, 200, 500)
	if err != nil {
		writeQuantProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	query := sdk.ScopeWorkflows(q.db.WithContext(c.Request.Context()), scope, "workflow_id").Where(
		"venue = ? AND market = ? AND instrument = ? AND interval = ?", series.Venue, series.Market, series.Instrument, series.Interval,
	)
	start, startSet, err := quantMarketSignalQueryTime(c.Request, "startTime")
	if err != nil {
		writeQuantProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	end, endSet, err := quantMarketSignalQueryTime(c.Request, "endTime")
	if err != nil || startSet && endSet && start.After(end) {
		writeQuantProblem(c, http.StatusBadRequest, "Quant market signal time range is invalid")
		return
	}
	if startSet {
		query = query.Where("candle_close_time >= ?", start)
	}
	if endSet {
		query = query.Where("candle_close_time <= ?", end)
	}
	var signals []quantMarketSignal
	if err := query.Order("candle_close_time DESC, id DESC").Limit(limit).Find(&signals).Error; err != nil {
		writeQuantProblem(c, http.StatusInternalServerError, "list Quant market signals failed")
		return
	}
	items := make([]map[string]any, len(signals))
	for index, signal := range signals {
		items[index] = map[string]any{
			"id": signal.ID, "venue": signal.Venue, "market": signal.Market, "instrument": signal.Instrument,
			"interval": signal.Interval, "name": signal.Name, "indicator": signal.Indicator,
			"candleCloseTime": signal.CandleCloseTime.UTC().Format(time.RFC3339Nano),
			"summary":         signal.Summary, "values": json.RawMessage(signal.Values),
			"createdAt": signal.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	writeQuantOK(c, map[string]any{"items": items})
}

func quantMarketSignalQueryTime(r *http.Request, key string) (time.Time, bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return time.Time{}, false, nil
	}
	value, err := parseQuantUTCTime(raw)
	if err != nil {
		return time.Time{}, false, errors.New("量化行情信号时间须为 RFC3339 格式的 UTC 时间")
	}
	return value, true, nil
}

var _ sdk.ActionHandler = quantMarketSignalAction{}
