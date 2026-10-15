package service

import (
	"coinsphere/backend/internal/db"
	"coinsphere/backend/plugin/sdk"
	"context"
	"crypto/rand"
	"fmt"
	"gorm.io/gorm"
	"strings"
	"time"
	"unicode/utf8"
)

const layoutComponent = "/index/index"

func (a *App) GetMenuTree(p *Principal) ([]M, error) {
	if !p.IsOwner() {
		return nil, ErrPermission
	}
	var menus []db.SystemMenu
	if err := a.DB.Where("is_active").Order("sort ASC, id ASC").Find(&menus).Error; err != nil {
		return nil, err
	}
	available := map[string]bool{}
	for _, page := range a.Plugins.NavigationPages() {
		available[page.RouteName()] = true
	}
	active := map[int64]db.SystemMenu{}
	for _, menu := range menus {
		active[menu.ID] = menu
	}
	visible := make([]db.SystemMenu, 0, len(menus))
	for _, menu := range menus {
		parent := menu.ParentID
		seen := map[int64]bool{menu.ID: true}
		ancestorsActive := true
		for parent != nil {
			item, found := active[*parent]
			if !found || seen[item.ID] {
				ancestorsActive = false
				break
			}
			seen[item.ID] = true
			parent = item.ParentID
		}
		if ancestorsActive && (menu.PluginID == nil || available[menu.Name]) {
			visible = append(visible, menu)
		}
	}
	return a.buildMenuTreePayload(visible), nil
}
func (a *App) GetMenuManagementTree() ([]M, error) {
	var menus []db.SystemMenu
	if err := a.DB.Order("sort ASC, id ASC").Find(&menus).Error; err != nil {
		return nil, err
	}
	return a.buildMenuTreePayload(menus), nil
}
func (a *App) GetI18nDict(scope string) M {
	result := M{"zh": M{}, "en": M{}}
	if scope != "menu" {
		return result
	}
	var rows []db.SystemI18nText
	a.DB.Where("locale IN ?", []string{"zh", "en"}).Find(&rows)
	for _, row := range rows {
		if localeMap, ok := result[row.Locale].(M); ok {
			localeMap[row.I18nKey] = row.Text
		}
	}
	return result
}

type I18nTextsPayload struct {
	Zh string `json:"zh"`
	En string `json:"en"`
}

// MenuUpsertPayload 菜单载荷。
type MenuUpsertPayload struct {
	PluginID         string           `json:"pluginId"`
	ContributionType string           `json:"contributionType"`
	PageKey          string           `json:"pageKey"`
	ParentID         *int64           `json:"parentId"`
	Title            string           `json:"title"`
	Icon             string           `json:"icon"`
	Sort             int              `json:"sort"`
	IsActive         *bool            `json:"isActive"`
	KeepAlive        bool             `json:"keepAlive"`
	IsHidden         bool             `json:"isHidden"`
	HideTab          bool             `json:"hideTab"`
	FixedTab         bool             `json:"fixedTab"`
	I18nTexts        I18nTextsPayload `json:"i18nTexts"`
}

// CreateMenu 创建菜单。
func (a *App) CreateMenu(ctx context.Context, payload MenuUpsertPayload) (M, error) {
	return a.upsertMenu(ctx, nil, payload)
}

// UpdateMenu 更新菜单。
func (a *App) UpdateMenu(ctx context.Context, menuID int64, payload MenuUpsertPayload) (M, error) {
	var menu db.SystemMenu
	if err := a.DB.First(&menu, menuID).Error; err != nil {
		return nil, bizErr("菜单不存在")
	}
	return a.upsertMenu(ctx, &menu, payload)
}

func (a *App) upsertMenu(ctx context.Context, existing *db.SystemMenu, payload MenuUpsertPayload) (M, error) {
	if strings.TrimSpace(payload.Title) == "" || utf8.RuneCountInString(payload.Title) > 100 || len(payload.Icon) > 100 || payload.Sort < 0 {
		return nil, bizErr("标题、图标或排序无效")
	}
	var id int64
	err := a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := commandPrincipalTx(tx, ctx); err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('coinsphere.navigation',0))").Error; err != nil {
			return err
		}
		menu := db.SystemMenu{Component: layoutComponent, Name: "Directory:" + rand.Text(), MenuType: "menu", IsActive: true, CreatedAt: time.Now().UTC()}
		if existing != nil {
			if err := tx.First(&menu, existing.ID).Error; err != nil {
				return err
			}
		}
		copyApp := &App{DB: tx, Plugins: a.Plugins}
		parent, err := copyApp.resolveParentMenu(payload.ParentID, menu.ID)
		if err != nil {
			return err
		}
		if payload.PluginID != "" || payload.ContributionType != "" || payload.PageKey != "" {
			if existing != nil && menu.PluginID == nil && menu.Component != layoutComponent {
				return bizErr("系统页面不能替换为插件页面")
			}
			var page *sdk.RegisteredPage
			for _, item := range a.Plugins.NavigationPages() {
				if item.PluginID == payload.PluginID && item.ContributionType == payload.ContributionType && item.PageKey == payload.PageKey {
					p := item
					page = &p
					break
				}
			}
			if page == nil {
				if menu.PluginID == nil || *menu.PluginID != payload.PluginID || menu.ContributionType == nil || *menu.ContributionType != payload.ContributionType || menu.PageKey == nil || *menu.PageKey != payload.PageKey {
					return bizErr("插件页面不存在")
				}
			} else {
				var duplicate int64
				if err := tx.Model(&db.SystemMenu{}).Where("plugin_id=? AND contribution_type=? AND page_key=? AND id<>?", page.PluginID, page.ContributionType, page.PageKey, menu.ID).Count(&duplicate).Error; err != nil {
					return err
				}
				if duplicate > 0 {
					return bizErr("该页面已绑定菜单")
				}
				var children int64
				if menu.ID > 0 {
					if err := tx.Model(&db.SystemMenu{}).Where("parent_id=?", menu.ID).Count(&children).Error; err != nil {
						return err
					}
				}
				if children > 0 {
					return bizErr("目录含有子菜单，不能绑定页面")
				}
				menu.PluginID, menu.ContributionType, menu.PageKey = &page.PluginID, &page.ContributionType, &page.PageKey
				menu.Name, menu.Path, menu.Component = page.RouteName(), page.RoutePath(), page.RouteComponent()
			}
		} else if menu.PluginID != nil {
			return bizErr("请选择插件页面")
		}
		menu.ParentID, menu.Title, menu.Icon, menu.Sort = parent, strings.TrimSpace(payload.Title), strings.TrimSpace(payload.Icon), payload.Sort
		menu.IsHidden, menu.KeepAlive, menu.IsHideTab = payload.IsHidden, payload.KeepAlive, payload.HideTab
		menu.FixedTab = payload.FixedTab
		if payload.IsActive != nil {
			menu.IsActive = *payload.IsActive
		}
		menu.UpdatedAt = time.Now().UTC()
		if menu.ID == 0 {
			if err := tx.Create(&menu).Error; err != nil {
				return err
			}
			if menu.PluginID == nil {
				menu.Name = fmt.Sprintf("Directory:%d", menu.ID)
				menu.Path = fmt.Sprintf("/menus/%d", menu.ID)
			}
		}
		if payload.IsActive != nil {
			menu.IsActive = *payload.IsActive
		}
		if err := tx.Save(&menu).Error; err != nil {
			return err
		}
		key := fmt.Sprintf("menus.custom.menu_%d", menu.ID)
		texts := payload.I18nTexts
		if strings.TrimSpace(texts.Zh) == "" {
			texts.Zh = menu.Title
		}
		if strings.TrimSpace(texts.En) == "" {
			texts.En = menu.Title
		}
		if err := copyApp.upsertI18nPair("menu", menu.ID, key, texts); err != nil {
			return err
		}
		id = menu.ID
		return nil
	})
	return M{"id": id}, err
}

// DeleteMenu shares the navigation lock with reparenting and page binding.
func (a *App) DeleteMenu(ctx context.Context, menuID int64) error {
	return a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := commandPrincipalTx(tx, ctx); err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('coinsphere.navigation',0))").Error; err != nil {
			return err
		}
		var menu db.SystemMenu
		if err := tx.First(&menu, menuID).Error; err != nil {
			return bizErr("菜单不存在")
		}
		ids, pending := []int64{menuID}, []int64{menuID}
		for len(pending) > 0 {
			current := pending[0]
			pending = pending[1:]
			var children []int64
			if err := tx.Model(&db.SystemMenu{}).Where("parent_id=?", current).Pluck("id", &children).Error; err != nil {
				return err
			}
			ids = append(ids, children...)
			pending = append(pending, children...)
		}
		if err := tx.Where("biz_type='menu' AND biz_id IN ?", ids).Delete(&db.SystemI18nText{}).Error; err != nil {
			return err
		}
		return tx.Delete(&menu).Error
	})
}

func (a *App) resolveParentMenu(parentID *int64, editingMenuID int64) (*int64, error) {
	if parentID == nil {
		return nil, nil
	}
	var parent db.SystemMenu
	if err := a.DB.First(&parent, *parentID).Error; err != nil {
		return nil, bizErr("菜单不存在")
	}
	if editingMenuID != 0 && parent.ID == editingMenuID {
		return nil, bizErr("父级菜单不能选择自己")
	}
	// 从候选父节点顺着 ParentID 一路往上"爬"到根:每爬一层就检查是否撞见正在编辑的菜单,
	// 撞见就说明选的是自己的后代,拒绝。current.ParentID != nil 表示还有上一层。
	if parent.Component != "" && parent.Component != layoutComponent || parent.PluginID != nil {
		return nil, bizErr("父级必须是目录")
	}
	seen := map[int64]bool{parent.ID: true}
	current := parent
	for current.ParentID != nil {
		if editingMenuID != 0 && *current.ParentID == editingMenuID {
			return nil, bizErr("父级菜单不能选择自己的子菜单")
		}
		if seen[*current.ParentID] {
			return nil, bizErr("菜单存在循环父级")
		}
		seen[*current.ParentID] = true
		var next db.SystemMenu
		if err := a.DB.First(&next, *current.ParentID).Error; err != nil {
			return nil, err
		}
		current = next
	}
	return &parent.ID, nil
}

// upsertI18nPair 对 zh / en 两条国际化文案做 upsert(有则更新、无则插入)。
func (a *App) upsertI18nPair(bizType string, bizID int64, i18nKey string, texts I18nTextsPayload) error {
	for locale, text := range map[string]string{"zh": strings.TrimSpace(texts.Zh), "en": strings.TrimSpace(texts.En)} {
		var row db.SystemI18nText
		err := a.DB.Where("biz_type = ? AND biz_id = ? AND locale = ?", bizType, bizID, locale).First(&row).Error
		if err == gorm.ErrRecordNotFound {
			row = db.SystemI18nText{BizType: bizType, BizID: bizID, I18nKey: i18nKey, Locale: locale, Text: text, UpdatedAt: time.Now().UTC()}
			if err := a.DB.Create(&row).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			updates := map[string]any{"i18n_key": i18nKey, "text": text, "updated_at": time.Now().UTC()}
			if err := a.DB.Model(&row).Updates(updates).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) listMenuI18n(menuIDs []int64) map[int64]M {
	result := map[int64]M{}
	var rows []db.SystemI18nText
	if len(menuIDs) == 0 {
		return result
	}
	a.DB.Where("biz_type = ? AND biz_id IN ?", "menu", menuIDs).Find(&rows)
	for _, row := range rows {
		key := row.BizID
		entry, ok := result[key]
		if !ok {
			entry = M{"i18nKey": row.I18nKey, "i18nTexts": M{"zh": "", "en": ""}}
			result[key] = entry
		}
		if texts, ok := entry["i18nTexts"].(M); ok && (row.Locale == "zh" || row.Locale == "en") {
			texts[row.Locale] = row.Text
		}
	}
	return result
}

func (a *App) buildMenuTreePayload(menus []db.SystemMenu) []M {
	menuIDs := collectIDs(menus, func(m db.SystemMenu) int64 { return m.ID })
	i18nMap := a.listMenuI18n(menuIDs)

	nodeMap := map[int64]M{}
	for _, menu := range menus {
		i18nPair := i18nMap[menu.ID]
		i18nKey, i18nTexts := extractI18n(i18nPair, menu.Title)
		meta := M{
			"title":   menu.Title,
			"i18nKey": i18nKey, "i18nTexts": i18nTexts,
			"keepAlive": menu.KeepAlive, "isHide": menu.IsHidden, "isHideTab": menu.IsHideTab,
			"isFullPage": menu.IsFullScreen, "isIframe": menu.UseIframe, "fixedTab": menu.FixedTab,
			"isEnable": menu.IsActive, "sort": menu.Sort,
			"pluginId": menu.PluginID, "contributionType": menu.ContributionType, "pageKey": menu.PageKey,
		}
		if menu.Icon != "" {
			meta["icon"] = menu.Icon
		}
		if menu.ActiveMenuPath != "" {
			meta["activePath"] = menu.ActiveMenuPath
		}
		if menu.ExternalURL != "" {
			meta["link"] = menu.ExternalURL
		}
		if menu.BadgeLabel != "" {
			meta["showBadge"] = true
			meta["showTextBadge"] = menu.BadgeLabel
		}

		component := menu.Component
		if component == "" && menu.PluginID == nil && menu.ExternalURL == "" && !menu.UseIframe {
			component = layoutComponent
		}
		var parentID any
		if menu.ParentID != nil {
			parentID = *menu.ParentID
		}
		nodeMap[menu.ID] = M{
			"id": menu.ID, "parentId": parentID, "path": menu.Path, "name": menu.Name,
			"component": component, "updatedAt": fmtTimeV(menu.UpdatedAt),
			"pluginId": menu.PluginID, "contributionType": menu.ContributionType, "pageKey": menu.PageKey,
			"meta": meta, "children": []M{},
		}
	}

	roots := []M{}
	for _, menu := range menus {
		node := nodeMap[menu.ID]
		if menu.ParentID != nil {
			if parent, ok := nodeMap[*menu.ParentID]; ok {
				parent["children"] = append(parent["children"].([]M), node)
				continue
			}
		}
		roots = append(roots, node)
	}
	for i := range roots {
		roots[i] = pruneEmptyChildren(roots[i])
	}
	return roots
}

func pruneEmptyChildren(node M) M {
	children, ok := node["children"].([]M)
	if !ok {
		return node
	}
	if len(children) == 0 {
		delete(node, "children")
		return node
	}
	for i := range children {
		children[i] = pruneEmptyChildren(children[i])
	}
	node["children"] = children
	return node
}

func extractI18n(pair M, fallbackTitle string) (string, M) {
	if pair == nil {
		return "", M{"zh": fallbackTitle, "en": fallbackTitle}
	}
	key, _ := pair["i18nKey"].(string)
	texts, _ := pair["i18nTexts"].(M)
	if texts == nil {
		texts = M{"zh": fallbackTitle, "en": fallbackTitle}
	}
	return key, texts
}

func collectIDs[T any](items []T, id func(T) int64) []int64 {
	result := make([]int64, 0, len(items))
	for _, item := range items {
		result = append(result, id(item))
	}
	return result
}

func (a *App) MenuPageCatalog() ([]M, error) {
	var menus []db.SystemMenu
	if err := a.DB.Where("plugin_id IS NOT NULL").Find(&menus).Error; err != nil {
		return nil, err
	}
	bound := map[string]int64{}
	for _, menu := range menus {
		bound[menu.Name] = menu.ID
	}
	result := []M{}
	for _, page := range a.Plugins.NavigationPages() {
		result = append(result, M{"pluginId": page.PluginID, "contributionType": page.ContributionType, "pageKey": page.PageKey, "title": page.Title, "icon": page.Icon, "menuId": bound[page.RouteName()]})
	}
	return result, nil
}
