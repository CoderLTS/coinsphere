package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"coinsphere/backend/plugin/sdk"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 直接结果查询使用当前请求范围，不创建固定视图。
func (a *App) ResolveDirectResultScope(ctx context.Context, pluginID, pageKey, action string, query url.Values, principal *Principal) (sdk.ResultScope, error) {
	page, ok := a.Plugins.ResultPage(pluginID, pageKey)
	if !ok {
		return sdk.ResultScope{}, ErrNotFound
	}
	scope, filters, err := resultQueryObjects(page, query)
	if err != nil {
		return sdk.ResultScope{}, err
	}
	result, err := a.directResultScopeTx(ctx, a.DB.WithContext(ctx), pluginID, pageKey, action, scope, filters, principal, false)
	if err != nil {
		return result, err
	}
	result.Revalidate = func(checkCtx context.Context, tx *gorm.DB) error {
		current, err := commandPrincipalTx(tx, WithPrincipal(checkCtx, principal))
		if err != nil {
			return err
		}
		_, err = a.directResultScopeTx(checkCtx, tx, pluginID, pageKey, action, scope, filters, current, true)
		return err
	}
	return result, nil
}

func (a *App) directResultScopeTx(ctx context.Context, tx *gorm.DB, pluginID, pageKey, action string, scope, filters json.RawMessage, principal *Principal, lock bool) (sdk.ResultScope, error) {
	p, err := principalForTx(tx, principal)
	if err != nil {
		return sdk.ResultScope{}, err
	}
	page, ok := a.Plugins.ResultPage(pluginID, pageKey)
	if !ok {
		return sdk.ResultScope{}, ErrNotFound
	}
	var status string
	installation := tx.Table("plugin_installations").Select("status").Where("plugin_id=?", pluginID)
	if lock {
		installation = tx.Raw("SELECT status FROM plugin_installations WHERE plugin_id=? FOR SHARE", pluginID)
	}
	if err := installation.Scan(&status).Error; err != nil {
		return sdk.ResultScope{}, err
	}
	if status != "installed" {
		return sdk.ResultScope{}, ErrNotFound
	}
	if action != "" && !containsString(page.Actions, action) {
		return sdk.ResultScope{}, ErrPermission
	}
	var scopeObject, filterObject any
	if json.Unmarshal(scope, &scopeObject) != nil || json.Unmarshal(filters, &filterObject) != nil || validateWorkflowSchemaValue(page.ScopeSchema, scopeObject) != nil || validateWorkflowSchemaValue(page.FilterSchema, filterObject) != nil {
		return sdk.ResultScope{}, errors.New("结果筛选不符合插件 Schema")
	}
	resources, err := page.Resources(scope)
	if err != nil {
		return sdk.ResultScope{}, err
	}
	for _, resource := range resources {
		if lock {
			var workflow struct{ ID int64 }
			if err := tx.Table("workflows").Select("id").Clauses(clause.Locking{Strength: "SHARE"}).Where("id=?", resource.WorkflowID).Take(&workflow).Error; err != nil {
				return sdk.ResultScope{}, ErrNotFound
			}
		}
		if err := authorizeWorkflowTx(tx, p, resource.WorkflowID); err != nil {
			return sdk.ResultScope{}, err
		}
	}
	if err := page.ValidateScope(ctx, tx, scope); err != nil {
		return sdk.ResultScope{}, fmt.Errorf("%w：结果资源", ErrNotFound)
	}
	return sdk.ResultScope{PluginID: pluginID, PageKey: pageKey, Scope: scope, Filters: filters, Resources: resources, UserID: p.User.ID, AllowedActions: append([]string(nil), page.Actions...)}, nil
}

func resultQueryObjects(page sdk.ResultPageDescriptor, query url.Values) (json.RawMessage, json.RawMessage, error) {
	scopeFields, _ := workflowSchemaProperties(page.ScopeSchema)
	filterFields, _ := workflowSchemaProperties(page.FilterSchema)
	scope, filters := map[string]any{}, map[string]any{}
	for key, values := range query {
		field, inScope := scopeFields[key]
		target := scope
		if !inScope {
			field = filterFields[key]
			target = filters
		}
		if field == nil || len(values) != 1 || len(values[0]) > 4096 {
			return nil, nil, errors.New("结果筛选字段未知或重复")
		}
		var value any = values[0]
		switch field["type"] {
		case "integer":
			parsed, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil {
				return nil, nil, errors.New("结果整数筛选无效")
			}
			value = parsed
		case "boolean":
			if values[0] != "true" && values[0] != "false" {
				return nil, nil, errors.New("结果布尔筛选无效")
			}
			value = values[0] == "true"
		case "number", "object", "array":
			if json.Unmarshal([]byte(values[0]), &value) != nil {
				return nil, nil, errors.New("结果 JSON 筛选无效")
			}
		}
		target[key] = value
	}
	scopeRaw, _ := json.Marshal(scope)
	filterRaw, _ := json.Marshal(filters)
	return scopeRaw, filterRaw, nil
}
