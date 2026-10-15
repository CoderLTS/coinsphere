package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"coinsphere/backend/internal/testdb"
	"coinsphere/backend/version"
)

func TestOfficialVersionSyncPreservesStatusAndReferences(t *testing.T) {
	database, _ := testdb.Open(t, true)
	ctx := context.Background()
	if _, err := database.Exec("UPDATE plugin_installations SET version='5.0.0'; UPDATE plugin_installations SET status='uninstalled' WHERE plugin_id='official.ai'; INSERT INTO plugin_references(plugin_id,reference_type,reference_id,active) VALUES('official.notification','revision','synthetic',true)"); err != nil {
		t.Fatal(err)
	}
	if err := applyOfficial(ctx, database, io.Discard); err == nil || !strings.Contains(err.Error(), "活动引用") {
		t.Fatal("version change ignored active reference", err)
	}
	var changed int
	if err := database.QueryRow("SELECT COUNT(*) FROM plugin_installations WHERE version <> '5.0.0'").Scan(&changed); err != nil || changed != 0 {
		t.Fatal("failed sync was not atomic", changed, err)
	}
	if _, err := database.Exec("UPDATE plugin_references SET active=false"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := applyOfficial(ctx, database, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM plugin_installations WHERE version=$1", version.Core).Scan(&count); err != nil || count != len(version.BuiltinCatalog) {
		t.Fatal("official versions were not synchronized", count, err)
	}
	var status string
	if err := database.QueryRow("SELECT status FROM plugin_installations WHERE plugin_id='official.ai'").Scan(&status); err != nil || status != "uninstalled" {
		t.Fatal("sync re-enabled an uninstalled plugin", status, err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM plugin_references WHERE NOT active").Scan(&count); err != nil || count != 1 {
		t.Fatal("sync changed historical references", count, err)
	}
}
