// Package migrations assembles the storage baselines owned by the official plugins.
package migrations

import (
	ai "coinsphere/backend/plugin/official/ai/migrations"
	binance "coinsphere/backend/plugin/official/binance/migrations"
	notification "coinsphere/backend/plugin/official/notification/migrations"
	quant "coinsphere/backend/plugin/official/quant/migrations"
	"io/fs"
)

type Bundle struct {
	ID, Schema string
	Files      fs.FS
}

func Bundles() []Bundle {
	a, _ := fs.Sub(ai.Files, "generation5")
	b, _ := fs.Sub(binance.Files, "generation5")
	n, _ := fs.Sub(notification.Files, "generation5")
	q, _ := fs.Sub(quant.Files, "generation5")
	return []Bundle{{"official.ai", "plugin_ai", a}, {"official.binance", "plugin_binance", b}, {"official.notification", "plugin_notification", n}, {"official.quant", "plugin_quant", q}}
}
