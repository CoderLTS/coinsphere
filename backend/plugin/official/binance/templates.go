package binance

import (
	"encoding/json"

	"coinsphere/backend/plugin/sdk"
)

func registerTemplates(registrar sdk.Registrar) error {
	for _, template := range []sdk.TemplateDescriptor{
		{
			Key: "binance-market-data", Name: "Binance 行情采集", Description: "采集 Binance Spot 或 USD-M 闭合 K 线。", Mode: "stream",
			Graph: json.RawMessage(`{"schemaVersion":3,"entryPoints":{"main":"market-stream"},"nodes":[{"nodeInstanceId":"market-stream","nodeType":"official.binance.realtime_candles","nodeVersion":"1.0.0","config":{"market":"spot","instrument":"BTCUSDT","intervals":["1h"]},"position":{"x":140,"y":220}},{"nodeInstanceId":"end","nodeType":"core.end","nodeVersion":"1.0.0","config":{},"position":{"x":520,"y":220}}],"edges":[{"edgeId":"market-end","sourceNodeInstanceId":"market-stream","sourcePort":"out","targetNodeInstanceId":"end","targetPort":"in"}]}`),
		},
	} {
		if err := registrar.Template(template); err != nil {
			return err
		}
	}
	return nil
}
