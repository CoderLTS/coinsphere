package db_test

import (
	"coinsphere/backend/internal/db"
	"coinsphere/backend/internal/security"
	"coinsphere/backend/internal/testdb"
	"coinsphere/backend/plugin/sdk"
	"context"
	"testing"
)

func TestSeedPreservesPersonalAccountAndUserNavigation(t *testing.T) {
	_, database := testdb.Open(t, true)
	ctx := context.Background()
	hasher := security.NewPasswordHasher(1)
	page := sdk.RegisteredPage{PluginID: "example.business", ContributionType: "result", Menu: sdk.PluginMenuDescriptor{Mode: sdk.PluginMenuOwn, Title: "业务"}, PageDescriptor: sdk.PageDescriptor{PageKey: "tasks", Title: "业务事项"}}
	if err := db.Seed(ctx, database, hasher, "synthetic", []sdk.RegisteredPage{page}); err != nil {
		t.Fatal(err)
	}
	var first db.SystemUser
	if err := database.First(&first).Error; err != nil {
		t.Fatal(err)
	}
	if first.Phone != "" || first.Email != "" || first.Location != "" || first.Company != "" || first.JobTitle != "" || first.Gender != "unknown" {
		t.Fatal("initial account contains invented personal data")
	}
	var menu db.SystemMenu
	if err := database.Where("name=?", page.RouteName()).First(&menu).Error; err != nil {
		t.Fatal(err)
	}
	if menu.Path != page.RoutePath() || menu.Component != page.RouteComponent() {
		t.Fatal("page reference routing lost")
	}
	if err := database.Model(&menu).Updates(map[string]any{"parent_id": nil, "title": "个人结果", "icon": "ri:home-line", "sort": 1, "is_hidden": true}).Error; err != nil {
		t.Fatal(err)
	}
	for _, pages := range [][]sdk.RegisteredPage{nil, {page}, {page, {PluginID: "example.business", ContributionType: "page", Menu: page.Menu, PageDescriptor: sdk.PageDescriptor{PageKey: "new", Title: "新页面"}}}} {
		if err := db.Seed(ctx, database, hasher, "different-initial-password", pages); err != nil {
			t.Fatal(err)
		}
		var user db.SystemUser
		if err := database.First(&user).Error; err != nil || user.ID != first.ID || user.PasswordHash != first.PasswordHash {
			t.Fatal("restart reset account", err)
		}
		var persisted db.SystemMenu
		if err := database.First(&persisted, menu.ID).Error; err != nil || persisted.ParentID != nil || persisted.Title != "个人结果" || persisted.Sort != 1 || !persisted.IsHidden || persisted.Icon != "ri:home-line" || !persisted.IsActive {
			t.Fatal("restart overwrote layout", err)
		}
	}
	var count int64
	if err := database.Model(&db.SystemMenu{}).Where("page_key='new'").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("upgrade auto-mounted a page", err)
	}
	if err := database.Model(&db.Workflow{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("initialization created workflows", err)
	}
}
