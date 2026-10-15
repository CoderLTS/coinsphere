package quant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"coinsphere/backend/plugin/contracts/trading"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/shopspring/decimal"
)

const smaStrategyID = "official.quant.sma-crossover"

var smaParameterSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"fastPeriod":{"type":"integer","minimum":1,"maximum":100,"title":"快线周期"},"slowPeriod":{"type":"integer","minimum":2,"maximum":200,"title":"慢线周期"}},"required":["fastPeriod","slowPeriod"],"additionalProperties":false}`)
var quantOne = decimal.NewFromInt(1)

var quantIntervals = map[string]time.Duration{
	"1m": time.Minute, "3m": 3 * time.Minute, "5m": 5 * time.Minute,
	"15m": 15 * time.Minute, "30m": 30 * time.Minute, "1h": time.Hour,
	"2h": 2 * time.Hour, "4h": 4 * time.Hour, "6h": 6 * time.Hour,
	"8h": 8 * time.Hour, "12h": 12 * time.Hour, "1d": 24 * time.Hour,
	"3d": 72 * time.Hour, "1w": 7 * 24 * time.Hour,
}

var quantIntervalOrder = []string{
	"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w",
}

type smaCrossoverStrategy struct{}

func (smaCrossoverStrategy) Descriptor() trading.StrategyDescriptor {
	return trading.StrategyDescriptor{
		ID: smaStrategyID, Version: "1.0.0", Name: "SMA crossover",
		ParameterSchema: smaParameterSchema, MinimumLookback: 2,
	}
}

func (smaCrossoverStrategy) Evaluate(ctx context.Context, request trading.EvaluateRequest) (decimal.Decimal, error) {
	if err := ctx.Err(); err != nil {
		return decimal.Zero, err
	}
	if err := validateStrategyCandles(request); err != nil {
		return decimal.Zero, err
	}
	var parameters struct {
		FastPeriod int `json:"fastPeriod"`
		SlowPeriod int `json:"slowPeriod"`
	}
	decoder := json.NewDecoder(bytes.NewReader(request.Parameters))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&parameters) != nil || parameters.FastPeriod < 1 || parameters.SlowPeriod <= parameters.FastPeriod ||
		parameters.FastPeriod > 100 || parameters.SlowPeriod > 200 || len(request.Candles) < parameters.SlowPeriod {
		return decimal.Zero, errors.New("均线交叉策略参数或回看周期无效")
	}
	fast := candleCloseAverage(request.Candles[len(request.Candles)-parameters.FastPeriod:])
	slow := candleCloseAverage(request.Candles[len(request.Candles)-parameters.SlowPeriod:])
	return decimal.NewFromInt(int64(fast.Cmp(slow))), nil
}

func candleCloseAverage(candles []trading.Candle) decimal.Decimal {
	total := decimal.Zero
	for _, candle := range candles {
		total = total.Add(candle.Close)
	}
	return total.Div(decimal.NewFromInt(int64(len(candles))))
}

func validateStrategyParameters(desc trading.StrategyDescriptor, parameters json.RawMessage) error {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(parameters))
	if err != nil {
		return errors.New("策略参数须为有效的 JSON")
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(desc.ParameterSchema))
	if err != nil {
		return errors.New("策略参数 Schema 无效")
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("parameters.json", document); err != nil {
		return errors.New("策略参数 Schema 无效")
	}
	schema, err := compiler.Compile("parameters.json")
	if err != nil || schema.Validate(value) != nil {
		return errors.New("策略参数不符合 Schema")
	}
	return nil
}

func validateStrategyCandles(request trading.EvaluateRequest) error {
	duration, ok := quantIntervals[request.Interval]
	if !ok || request.Market != "spot" && request.Market != "usdm" || request.Instrument == "" || request.EvaluatedAt.IsZero() {
		return errors.New("策略上下文无效")
	}
	_, evaluatedOffset := request.EvaluatedAt.Zone()
	if evaluatedOffset != 0 || len(request.Candles) == 0 {
		return errors.New("策略计算需要已收盘且时间使用 UTC 的 K 线")
	}
	for index, candle := range request.Candles {
		_, openOffset := candle.OpenTime.Zone()
		_, closeOffset := candle.CloseTime.Zone()
		if openOffset != 0 || closeOffset != 0 || candle.OpenTime.IsZero() || !candle.CloseTime.After(candle.OpenTime) ||
			candle.CloseTime.After(request.EvaluatedAt) || candle.Open.Sign() <= 0 || candle.High.Sign() <= 0 ||
			candle.Low.Sign() <= 0 || candle.Close.Sign() <= 0 || candle.Volume.Sign() < 0 ||
			candle.High.LessThan(decimal.Max(candle.Open, decimal.Max(candle.Close, candle.Low))) ||
			candle.Low.GreaterThan(decimal.Min(candle.Open, decimal.Min(candle.Close, candle.High))) {
			return errors.New("策略 K 线无效或尚未收盘")
		}
		if index > 0 && !candle.OpenTime.Equal(request.Candles[index-1].OpenTime.Add(duration)) {
			return fmt.Errorf("策略 K 线在 %s 存在缺口", candle.OpenTime.UTC().Format(time.RFC3339))
		}
	}
	return nil
}

var _ trading.Strategy = smaCrossoverStrategy{}
