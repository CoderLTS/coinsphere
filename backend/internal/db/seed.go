package db

import (
	"coinsphere/backend/internal/security"
	"coinsphere/backend/plugin/sdk"
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"sort"
	"time"
)

type menuItem struct {
	Name, Title, Path, Component, Icon, Parent string
	KeepAlive, FixedTab, IsHidden              bool
	Page                                       *sdk.RegisteredPage
}

var coreMenuItems = []menuItem{
	{Name: "Home", Title: "首页", Path: "/home", Component: "/home/index", Icon: "ri:home-5-line", KeepAlive: true, FixedTab: true},
	{Name: "SchedulerCenter", Title: "工作流", Path: "/scheduler", Component: "/index/index", Icon: "ri:time-line"},
	{Name: "WorkflowDefinitions", Title: "工作流定义", Path: "/scheduler/definition", Component: "/scheduler/workflow", Icon: "ri:node-tree", Parent: "SchedulerCenter", KeepAlive: true},
	{Name: "ConfigCenter", Title: "配置管理", Path: "/config", Component: "/index/index", Icon: "ri:tools-line"},
	{Name: "OutboundProxies", Title: "代理配置", Path: "/config/proxies", Component: "/system/proxy", Icon: "ri:route-line", Parent: "ConfigCenter", KeepAlive: true},
	{Name: "Plugins", Title: "插件管理", Path: "/config/plugins", Component: "/system/plugins", Icon: "ri:puzzle-2-line", Parent: "ConfigCenter", KeepAlive: true},
	{Name: "System", Title: "系统管理", Path: "/system", Component: "/index/index", Icon: "ri:settings-3-line"},
	{Name: "Menus", Title: "菜单管理", Path: "/system/menu", Component: "/system/menu", Icon: "ri:menu-line", Parent: "System", KeepAlive: true},
	{Name: "UserCenter", Title: "个人中心", Path: "/profile", Component: "/system/user-center", KeepAlive: true, IsHidden: true},
}

// 只有空库首次初始化写默认布局；后续启动不触碰用户的导航配置。
func Seed(ctx context.Context, gdb *gorm.DB, hasher *security.PasswordHasher, password string, pages []sdk.RegisteredPage) error {
	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := seedPersonalUser(tx, hasher, password); err != nil {
			return err
		}
		marker := tx.Exec("INSERT INTO navigation_initialization(id) VALUES(TRUE) ON CONFLICT DO NOTHING")
		if marker.Error != nil {
			return marker.Error
		}
		if marker.RowsAffected == 0 {
			return nil
		}
		items := append([]menuItem(nil), coreMenuItems[:3]...)
		own := map[string]bool{}
		pluginOrder := map[string]int{"official.ai": 1, "official.quant": 2, "official.binance": 3}
		pages = append([]sdk.RegisteredPage(nil), pages...)
		sort.SliceStable(pages, func(i, j int) bool {
			left, right := pluginOrder[pages[i].PluginID], pluginOrder[pages[j].PluginID]
			if left != right {
				return left < right
			}
			return pages[i].RouteName() < pages[j].RouteName()
		})
		for _, page := range pages {
			if page.Menu.Mode != sdk.PluginMenuOwn || own[page.PluginID] {
				continue
			}
			own[page.PluginID] = true
			items = append(items, menuItem{Name: "PluginMenu:" + page.PluginID, Title: page.Menu.Title, Path: "/plugins/" + page.PluginID, Component: "/index/index", Icon: page.Menu.Icon})
		}
		items = append(items, coreMenuItems[3:]...)
		for _, item := range pages {
			page := item
			parent := ""
			switch page.Menu.Mode {
			case sdk.PluginMenuExisting:
				parent = page.Menu.Parent
			case sdk.PluginMenuDirect:
			default:
				parent = "PluginMenu:" + page.PluginID
			}
			items = append(items, menuItem{Name: page.RouteName(), Title: page.Title, Path: page.RoutePath(), Component: page.RouteComponent(), Icon: page.Icon, Parent: parent, KeepAlive: page.KeepAlive, Page: &page})
		}
		ids := map[string]int64{}
		for index, item := range items {
			menu := SystemMenu{Name: item.Name, Path: item.Path, Title: item.Title, Component: item.Component, Icon: item.Icon, Sort: (index + 1) * 10, KeepAlive: item.KeepAlive, FixedTab: item.FixedTab, IsHidden: item.IsHidden, IsActive: true, MenuType: "menu", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
			if item.Parent != "" {
				id, found := ids[item.Parent]
				if !found {
					return fmt.Errorf("default menu parent %s is unavailable", item.Parent)
				}
				menu.ParentID = &id
			}
			if item.Page != nil {
				menu.PluginID = &item.Page.PluginID
				menu.ContributionType = &item.Page.ContributionType
				menu.PageKey = &item.Page.PageKey
			}
			if err := tx.Create(&menu).Error; err != nil {
				return err
			}
			ids[item.Name] = menu.ID
			if err := upsertI18nPair(tx, "menu", menu.ID, fmt.Sprintf("menus.custom.menu_%d", menu.ID), item.Title, item.Title); err != nil {
				return err
			}
		}
		return nil
	})
}

func seedPersonalUser(tx *gorm.DB, hasher *security.PasswordHasher, adminPassword string) (*SystemUser, error) {
	now := time.Now().UTC()
	tags, _ := json.Marshal([]string{"coinsphere", "system", "personal"})
	var user SystemUser
	err := tx.Where("username = ?", "coinsphere").First(&user).Error
	if err == gorm.ErrRecordNotFound {
		user = SystemUser{
			Username: "coinsphere", PasswordHash: hasher.HashPassword(adminPassword),
			Nickname: "coinsphere", FullName: "coinsphere", Gender: "unknown",
			IsActive: true,
			Bio:      "个人工作流系统。", TagsJSON: string(tags),
			CreatedBy: "system", UpdatedBy: "system", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&user).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return &user, nil
}

func upsertI18nPair(tx *gorm.DB, bizType string, bizID int64, key, zh, en string) error {
	for locale, text := range map[string]string{"zh": zh, "en": en} {
		var row SystemI18nText
		err := tx.Where("biz_type = ? AND biz_id = ? AND locale = ?", bizType, bizID, locale).First(&row).Error
		if err == gorm.ErrRecordNotFound {
			row = SystemI18nText{BizType: bizType, BizID: bizID, I18nKey: key, Locale: locale, Text: text, UpdatedAt: time.Now().UTC()}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			updates := map[string]any{"i18n_key": key, "text": text, "updated_at": time.Now().UTC()}
			if err := tx.Model(&row).Updates(updates).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
