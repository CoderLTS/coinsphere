package service

import (
	"coinsphere/backend/internal/db"
	"coinsphere/backend/plugin/sdk"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"sort"
	"strings"
)

func (a *App) PluginCatalog(ctx context.Context) []M {
	items := []M{}
	for _, plugin := range a.Plugins.Plugins() {
		pages := []M{}
		for _, page := range a.Plugins.PluginResultPages(plugin.ID) {
			pages = append(pages, M{"pageKey": page.PageKey, "title": page.Title, "componentEntry": page.ComponentEntry, "configComponentEntry": page.ConfigComponentEntry, "scopeSchema": page.ScopeSchema, "filterSchema": page.FilterSchema, "actions": page.Actions})
		}
		panels := []M{}
		for _, panel := range a.Plugins.RunPanels(plugin.ID) {
			panels = append(panels, M{"panelKey": panel.PanelKey, "title": panel.Title, "nodeTypes": panel.NodeTypes, "componentEntry": panel.ComponentEntry})
		}
		ordinary := []M{}
		for _, page := range a.Plugins.Pages() {
			if page.PluginID == plugin.ID {
				ordinary = append(ordinary, M{"pageKey": page.PageKey, "title": page.Title, "icon": page.Icon})
			}
		}
		headers := []sdk.RegisteredHeaderAction{}
		for _, action := range a.Plugins.HeaderActions() {
			if action.PluginID == plugin.ID {
				headers = append(headers, action)
			}
		}
		items = append(items, M{"id": plugin.ID, "name": plugin.Name, "version": plugin.Version, "pages": ordinary, "headerActions": headers, "resultPages": pages, "runPanels": panels})
	}
	return items
}
func (a *App) PageWorkflows(ctx context.Context, page CursorPage, status, keyword string, groupID *int64) (M, error) {
	if err := requireAccount(ctx); err != nil {
		return nil, err
	}
	query := workflowScopeQuery(a.DB.WithContext(ctx).Model(&db.Workflow{}), ContextPrincipal(ctx))
	if status != "" {
		if !validWorkflowStatus(status) {
			return nil, errors.New("invalid workflow status")
		}
		query = query.Where("workflows.status=?", status)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		query = query.Where("workflows.name ILIKE ?", "%"+keyword+"%")
	}
	if groupID != nil {
		if *groupID == 0 {
			query = query.Where("workflows.group_id IS NULL")
		} else {
			query = query.Where("workflows.group_id=?", *groupID)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	after, err := page.AfterID()
	if err != nil {
		return nil, err
	}
	if after > 0 {
		query = query.Where("workflows.id<?", after)
	}
	var rows []struct {
		db.Workflow
		MaxConcurrentRuns int
		BacklogLimit      int
		LatestRunID       *int64
		LatestRunStatus   *string
	}
	err = query.Select("workflows.*, rt.max_concurrent_runs, rt.backlog_limit, recent.id AS latest_run_id, recent.status AS latest_run_status").Joins("JOIN workflow_runtimes rt ON rt.workflow_id=workflows.id").Joins("LEFT JOIN LATERAL (SELECT id,status FROM workflow_runs r WHERE r.workflow_id=workflows.id ORDER BY id DESC LIMIT 1) recent ON TRUE").Order("workflows.id DESC").Limit(page.Limit + 1).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	more := len(rows) > page.Limit
	if more {
		rows = rows[:page.Limit]
	}
	items := []M{}
	for _, r := range rows {
		raw, _ := json.Marshal(workflowView(r.Workflow))
		item := M{}
		_ = json.Unmarshal(raw, &item)
		item["maxConcurrentRuns"], item["backlogLimit"] = r.MaxConcurrentRuns, r.BacklogLimit
		item["latestRunId"], item["latestRunStatus"] = r.LatestRunID, r.LatestRunStatus
		items = append(items, item)
	}
	key := ""
	if len(rows) > 0 {
		key = fmt.Sprint(rows[len(rows)-1].ID)
	}
	return cursorResult(items, page, key, more, total), nil
}
func (a *App) ResolveSystemScope(ctx context.Context, pluginID string) (sdk.SystemScope, error) {
	if err := requireAccount(ctx); err != nil {
		return sdk.SystemScope{}, err
	}
	p := ContextPrincipal(ctx)
	scope := sdk.SystemScope{PluginID: pluginID, UserID: p.User.ID, AllWorkflows: true}
	scope.SessionValid = func(checkCtx context.Context) error {
		_, err := a.RevalidateSession(p)
		return err
	}
	return scope, nil
}
func (a *App) ResolveWorkflowScope(ctx context.Context, pluginID string, id int64, nodeID string) (sdk.WorkflowScope, error) {
	if err := requireAccount(ctx); err != nil {
		return sdk.WorkflowScope{}, err
	}
	if err := a.AuthorizeWorkflow(ctx, id); err != nil {
		return sdk.WorkflowScope{}, err
	}
	var w db.Workflow
	if err := a.DB.WithContext(ctx).First(&w, id).Error; err != nil {
		return sdk.WorkflowScope{}, err
	}
	revisionID := revisionPointerValue(w.DraftRevisionID)
	var r db.WorkflowRevision
	if err := a.DB.WithContext(ctx).First(&r, revisionID).Error; err != nil {
		return sdk.WorkflowScope{}, err
	}
	g, err := a.validateWorkflowGraph(json.RawMessage(r.GraphJSON))
	if err != nil || a.Plugins.NodePlugin(g.nodes[nodeID].NodeType) != pluginID {
		return sdk.WorkflowScope{}, ErrNotFound
	}
	return sdk.WorkflowScope{PluginID: pluginID, WorkflowID: fmt.Sprint(id), RevisionID: fmt.Sprint(revisionID), NodeInstanceID: nodeID, UserID: ContextPrincipal(ctx).User.ID}, nil
}
func (a *App) authorizeArtifact(ctx context.Context, digest string) error {
	query := a.DB.WithContext(ctx).Table("workflow_artifact_refs ref").Joins("JOIN workflow_run_nodes n ON n.id=ref.run_node_id").Joins("JOIN workflow_runs r ON r.id=n.run_id").Where("ref.artifact_sha256=?", digest)
	query = workflowScopeQuery(query, ContextPrincipal(ctx))
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
func (a *App) syncRunPluginReferences(tx *gorm.DB, run db.WorkflowRun, g validatedWorkflowGraph) error {
	ids := map[string]bool{}
	for _, typ := range g.nodeTypes {
		if id := a.Plugins.NodePlugin(typ); id != "" {
			ids[id] = true
		}
	}
	sorted := []string{}
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		if err := a.addPluginReference(tx, id, "run", fmt.Sprint(run.ID)); err != nil {
			return err
		}
	}
	return nil
}
