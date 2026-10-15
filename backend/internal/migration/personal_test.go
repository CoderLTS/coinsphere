package migration_test

import (
	"coinsphere/backend/internal/migration"
	"coinsphere/backend/internal/testdb"
	"context"
	"testing"
)

func TestGenerationFiveEmptyDatabaseAndRemovedTables(t *testing.T) {
	ctx := context.Background()
	database, _ := testdb.Open(t, false)
	runner, err := migration.New(database)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := runner.Up(ctx, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := runner.ValidateCurrent(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
		t.Fatal("migration seeded accounts", err)
	}
	if _, err := database.Exec("INSERT INTO users(username,password_hash) VALUES('coinsphere','synthetic')"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"INSERT INTO users(username) VALUES('other')", "INSERT INTO users(username) VALUES('coinsphere')", "UPDATE users SET username='other'", "INSERT INTO menus(name,plugin_id,page_key) VALUES('invalid','synthetic','tasks')"} {
		if _, err := database.Exec(q); err == nil {
			t.Fatal("baseline invariant bypassed", q)
		}
	}
	for _, table := range []string{"roles", "permissions", "workflow_user_grants", "result_view_role_grants", "result_views", "ai_model_configs", "assistant_sessions"} {
		var exists bool
		if err := database.QueryRow("SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&exists); err != nil || exists {
			t.Fatal("removed core table", table, err)
		}
	}
	if _, err := runner.Down(ctx, 1); err == nil {
		t.Fatal("destructive baseline rollback accepted")
	}
	if err := runner.ValidateCurrent(ctx); err != nil {
		t.Fatal("failed rollback changed generation", err)
	}
}

func TestWrongGenerationRejected(t *testing.T) {
	ctx := context.Background()
	database, _ := testdb.Open(t, false)
	if _, err := database.Exec("CREATE TABLE schema_generation(id boolean PRIMARY KEY,generation integer NOT NULL); INSERT INTO schema_generation VALUES(TRUE,4)"); err != nil {
		t.Fatal(err)
	}
	if err := migration.ValidateGeneration(ctx, database); err == nil {
		t.Fatal("wrong generation accepted")
	}
	if _, err := database.Exec("CREATE TABLE schema_migrations_g4(id integer)"); err != nil {
		t.Fatal(err)
	}
	if _, err := migration.New(database); err == nil {
		t.Fatal("legacy ledger accepted")
	}
}
