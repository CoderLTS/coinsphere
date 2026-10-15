package service

import (
	"encoding/json"
	"strings"
	"testing"

	"coinsphere/backend/internal/db"
	"coinsphere/backend/plugin/sdk"
)

func TestNavigationPlacementPersistenceAndAvailability(t *testing.T) {
	a, ctx, p := platformFixture(t, nil)
	pages := a.Plugins.NavigationPages()
	if err := db.Seed(ctx, a.DB, a.Hasher, "synthetic", pages); err != nil {
		t.Fatal(err)
	}
	var page db.SystemMenu
	if err := a.DB.Where("plugin_id='test.business'").First(&page).Error; err != nil {
		t.Fatal(err)
	}
	originalPath, originalName := page.Path, page.Name
	createDirectory := func(title string, parent *int64) int64 {
		t.Helper()
		result, err := a.CreateMenu(ctx, MenuUpsertPayload{Title: title, ParentID: parent})
		if err != nil {
			t.Fatal(err)
		}
		return result["id"].(int64)
	}
	first := createDirectory("我的目录", nil)
	second := createDirectory("二级目录", &first)
	payload := MenuUpsertPayload{ParentID: &second, PluginID: *page.PluginID, ContributionType: *page.ContributionType, PageKey: *page.PageKey, Title: "我的事项", Icon: "ri:bookmark-line", Sort: 7, IsHidden: true}
	if _, err := a.UpdateMenu(ctx, page.ID, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateMenu(ctx, payload); err == nil {
		t.Fatal("duplicate page binding accepted")
	}
	if _, err := a.UpdateMenu(ctx, first, MenuUpsertPayload{Title: "循环", ParentID: &second}); err == nil {
		t.Fatal("ancestor cycle accepted")
	}
	if _, err := a.CreateMenu(ctx, MenuUpsertPayload{Title: "页面的孩子", ParentID: &page.ID}); err == nil {
		t.Fatal("page accepted children")
	}
	for _, parent := range []*int64{nil, &first, &second} {
		payload.ParentID = parent
		if _, err := a.UpdateMenu(ctx, page.ID, payload); err != nil {
			t.Fatal(err)
		}
		if err := a.DB.First(&page, page.ID).Error; err != nil {
			t.Fatal(err)
		}
		if page.Path != originalPath || page.Name != originalName {
			t.Fatal("moving a page changed its address")
		}
	}
	registry := a.Plugins
	a.Plugins = sdk.NewRegistry()
	tree, err := a.GetMenuTree(p)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(tree)
	if strings.Contains(string(raw), originalName) {
		t.Fatal("missing plugin page remained available")
	}
	management, err := a.GetMenuManagementTree()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(management)
	if !strings.Contains(string(raw), originalName) {
		t.Fatal("missing plugin layout was deleted")
	}
	if _, err := a.UpdateMenu(ctx, page.ID, payload); err != nil {
		t.Fatal("cannot edit preserved missing page", err)
	}
	if err := db.Seed(ctx, a.DB, a.Hasher, "ignored-new-password", nil); err != nil {
		t.Fatal(err)
	}
	a.Plugins = registry
	if err := db.Seed(ctx, a.DB, a.Hasher, "ignored-new-password", pages); err != nil {
		t.Fatal(err)
	}
	if err := a.DB.First(&page, page.ID).Error; err != nil {
		t.Fatal(err)
	}
	if page.ParentID == nil || *page.ParentID != second || page.Title != "我的事项" || page.Sort != 7 || !page.IsHidden || page.Icon != "ri:bookmark-line" {
		t.Fatal("reload overwrote user layout")
	}
	disabled := false
	if _, err := a.UpdateMenu(ctx, first, MenuUpsertPayload{Title: "我的目录", IsActive: &disabled}); err != nil {
		t.Fatal(err)
	}
	tree, err = a.GetMenuTree(p)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(tree)
	if strings.Contains(string(raw), originalName) {
		t.Fatal("child of disabled ancestor became a root")
	}
	if err := a.DeleteMenu(ctx, first); err != nil {
		t.Fatal(err)
	}
	if countTestRows(t, a, "menus", "id IN ?", []int64{first, second, page.ID}) != 0 || countTestRows(t, a, "i18n_texts", "biz_type='menu' AND biz_id IN ?", []int64{first, second, page.ID}) != 0 {
		t.Fatal("subtree deletion left rows")
	}
	catalog, err := a.MenuPageCatalog()
	if err != nil || len(catalog) != 1 || catalog[0]["menuId"].(int64) != 0 {
		t.Fatal("deleted page not returned to unmounted catalog", err)
	}
}
