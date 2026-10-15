package generation5_test

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"testing"

	"coinsphere/backend/internal/testdb"
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var core embed.FS

func TestEmptyGeneration5Baseline(t *testing.T) {
	database, _ := testdb.Open(t, false)
	ctx := context.Background()
	provider, err := goose.NewProvider(goose.DialectPostgres, database, core, goose.WithTableName("schema_migrations_g5"), goose.WithDisableGlobalRegistry(true), goose.WithLogger(goose.NopLogger()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if result, err := provider.Up(ctx); err != nil || len(result) != 0 {
		t.Fatal("baseline repeated initialization", err)
	}
	var generation int
	if err = database.QueryRow("SELECT generation FROM schema_generation").Scan(&generation); err != nil || generation != 5 {
		t.Fatal("wrong generation", err)
	}
	for _, table := range []string{"roles", "permissions", "role_permissions", "workflow_permissions", "result_views", "ai_model_configs", "assistant_sessions"} {
		var absent bool
		if err = database.QueryRow("SELECT to_regclass($1) IS NULL", "public."+table).Scan(&absent); err != nil || !absent {
			t.Fatal("legacy or business table in core", table, err)
		}
	}
	if _, err = database.Exec("INSERT INTO users(username) VALUES('coinsphere')"); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("INSERT INTO users(username) VALUES('coinsphere')"); err == nil {
		t.Fatal("second account accepted")
	}
	if _, err = database.Exec("INSERT INTO menus(name,plugin_id,contribution_type,page_key) VALUES('first','example.business','result','tasks')"); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("INSERT INTO menus(name,plugin_id,contribution_type,page_key) VALUES('duplicate','example.business','result','tasks')"); err == nil {
		t.Fatal("duplicate page binding accepted")
	}
	if _, err = database.Exec("INSERT INTO i18n_texts(biz_type,biz_id,i18n_key,locale) SELECT 'menu',id,'menus.test','zh' FROM menus WHERE name='first'"); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"INSERT INTO i18n_texts(biz_type,biz_id,i18n_key,locale) SELECT 'button',id,'menus.button','zh' FROM menus WHERE name='first'",
		"INSERT INTO i18n_texts(biz_type,biz_id,i18n_key,locale) VALUES('menu',9223372036854775807,'menus.missing','zh')",
	} {
		if _, err = database.Exec(query); err == nil {
			t.Fatal("translation without a menu accepted")
		}
	}
	if _, err = database.Exec("DELETE FROM menus WHERE name='first'"); err != nil {
		t.Fatal(err)
	}
	var translations int
	if err = database.QueryRow("SELECT count(*) FROM i18n_texts").Scan(&translations); err != nil || translations != 0 {
		t.Fatal("deleting menu left translations", err)
	}
	for _, name := range []string{"ai", "binance", "notification", "quant"} {
		schema := "plugin_" + name
		if _, err = database.Exec("CREATE SCHEMA " + schema); err != nil {
			t.Fatal(err)
		}
		files := os.DirFS(filepath.Join("..", "..", "..", "plugin", "official", name, "migrations", "generation5"))
		plugin, err := goose.NewProvider(goose.DialectPostgres, database, files, goose.WithTableName(schema+".schema_migrations_g5"), goose.WithDisableGlobalRegistry(true), goose.WithLogger(goose.NopLogger()))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = plugin.Up(ctx); err != nil {
			t.Fatal(name, err)
		}
		if result, err := plugin.Up(ctx); err != nil || len(result) != 0 {
			t.Fatal("plugin baseline repeated initialization", name, err)
		}
	}
	if _, err = provider.Down(ctx); err == nil {
		t.Fatal("destructive in-place rollback accepted")
	}
	if err = database.QueryRow("SELECT generation FROM schema_generation").Scan(&generation); err != nil || generation != 5 {
		t.Fatal("failed rollback changed generation", err)
	}
}
