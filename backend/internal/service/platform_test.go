package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coinsphere/backend/internal/config"
	"coinsphere/backend/internal/db"
	"coinsphere/backend/internal/testdb"
	"coinsphere/backend/plugin/sdk"
	workflowgraph "coinsphere/backend/workflow/graph"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type testAction func(context.Context, sdk.ActionRequest) (sdk.ActionResult, error)

func (f testAction) Execute(ctx context.Context, r sdk.ActionRequest) (sdk.ActionResult, error) {
	return f(ctx, r)
}

// A non-financial plugin exercises every extension through the real registry.
func businessPlugin(t *testing.T, registry *sdk.Registry, action testAction) {
	t.Helper()
	empty := json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false}`)
	err := registry.RegisterPlugin(sdk.PluginDescriptor{ID: "test.business", Name: "Business", Version: "1.0.0", Contributes: []string{"nodes", "apiRoutes", "resultPages", "runPanels"}}, sdk.Host{}, func(r sdk.Registrar, _ sdk.Host) error {
		for _, kind := range []string{"task", "state", "external"} {
			desc := sdk.NodeDescriptor{Type: "test.business." + kind, Version: "1.0.0", Kind: sdk.NodeKindAction, Title: kind, Description: "Synthetic business node", Category: "business", Color: "#2563eb", Icon: "task", Width: 220, Height: 72, Pool: sdk.PoolStream, State: sdk.StateStateless, SideEffect: sdk.SideEffectNone, ConfigSchema: empty, InputSchema: empty, OutputSchema: empty, UISchema: json.RawMessage(`{"ui:order":[]}`)}
			if kind == "state" {
				desc.State = sdk.StatePersistent
			}
			if kind == "external" {
				desc.SideEffect = sdk.SideEffectExternal
			}
			if err := r.Action(desc, action); err != nil {
				return err
			}
		}
		if err := r.RunPanel(sdk.RunPanelDescriptor{PanelKey: "task", Title: "Task", NodeTypes: []string{"test.business.task"}, ComponentEntry: "./TaskPanel.vue"}); err != nil {
			return err
		}
		resources := func(raw json.RawMessage) ([]sdk.WorkflowResource, error) {
			var s struct {
				WorkflowID int64  `json:"workflowId"`
				NodeID     string `json:"nodeId"`
			}
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, err
			}
			return []sdk.WorkflowResource{{WorkflowID: s.WorkflowID, NodeInstanceID: s.NodeID}}, nil
		}
		if err := r.ResultPage(sdk.ResultPageDescriptor{PageKey: "tasks", Title: "Tasks", ComponentEntry: "./TaskResults.vue", Actions: []string{"ack"}, ScopeSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"workflowId":{"type":"integer","minimum":1},"nodeId":{"type":"string"}},"required":["workflowId","nodeId"],"additionalProperties":false}`), FilterSchema: empty, Resources: resources, ValidateScope: func(ctx context.Context, tx *gorm.DB, raw json.RawMessage) error {
			refs, err := resources(raw)
			if err != nil {
				return err
			}
			var revision db.WorkflowRevision
			if err := tx.WithContext(ctx).Joins("JOIN workflows w ON w.draft_revision_id=workflow_revisions.id").Where("w.id=?", refs[0].WorkflowID).First(&revision).Error; err != nil {
				return err
			}
			var graph workflowGraph
			if err := json.Unmarshal([]byte(revision.GraphJSON), &graph); err != nil {
				return err
			}
			for _, node := range graph.Nodes {
				if node.NodeInstanceID == refs[0].NodeInstanceID && node.NodeType == "test.business.task" {
					return nil
				}
			}
			return ErrNotFound
		}}); err != nil {
			return err
		}
		for _, desc := range []sdk.RouteDescriptor{{Scope: sdk.ScopeWorkflow, Method: http.MethodGet, Pattern: "/summary"}, {Scope: sdk.ScopeResult, ResultPageKey: "tasks", Method: http.MethodPost, Pattern: "/ack", Action: "ack"}} {
			if err := r.Route(desc, func(c *gin.Context, scope sdk.RouteScope) { c.JSON(http.StatusOK, gin.H{"scoped": scope != nil}) }); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowNodeCatalogNames(t *testing.T) {
	registry := sdk.NewRegistry()
	businessPlugin(t, registry, func(context.Context, sdk.ActionRequest) (sdk.ActionResult, error) {
		return sdk.ActionResult{Output: json.RawMessage(`{}`)}, nil
	})
	app := App{Plugins: registry}
	found := false
	for _, node := range app.ListWorkflowNodeDefinitions() {
		if node.Type == "test.business.task" {
			found = true
			if node.PluginID != "test.business" || node.PluginName != "Business" {
				t.Fatalf("missing plugin identity: %#v", node)
			}
		}
	}
	if !found {
		t.Fatal("business node missing from catalog")
	}
	core := App{}
	for _, node := range core.ListWorkflowNodeDefinitions() {
		if node.CategoryTitle == "" || node.PluginID != "" || node.PluginName != "" {
			t.Fatalf("invalid core node identity: %#v", node)
		}
	}
}

func platformFixture(t *testing.T, action testAction) (*App, context.Context, *Principal) {
	t.Helper()
	_, database := testdb.Open(t, true)
	registry := sdk.NewRegistry()
	if action == nil {
		action = func(context.Context, sdk.ActionRequest) (sdk.ActionResult, error) {
			return sdk.ActionResult{Output: json.RawMessage(`{}`)}, nil
		}
	}
	businessPlugin(t, registry, action)
	app := NewApp(database, &config.AppConfig{Auth: config.AuthConfig{SecretKey: "synthetic-platform-key", PasswordIterations: 1, AccessTokenTTLMinutes: 60}}, registry)
	app.ArtifactRoot = t.TempDir()
	for _, sql := range []string{`INSERT INTO users(id,username) VALUES(1,'coinsphere')`, `INSERT INTO plugin_installations(plugin_id,version,schema_name,source_path,status) VALUES('test.business','1.0.0','plugin_test_business','synthetic','installed')`} {
		if err := database.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	p, err := app.buildPrincipal(1)
	if err != nil {
		t.Fatal(err)
	}
	return app, WithPrincipal(context.Background(), p), p
}

func testGraph(kind string) workflowGraph {
	nodes := []workflowGraphNode{{NodeInstanceID: "manual", NodeType: "core.manual", NodeVersion: "1.0.0", Config: json.RawMessage(`{}`), Position: &workflowgraph.Position{}}}
	if kind != "" {
		nodes = append(nodes, workflowGraphNode{NodeInstanceID: "task", NodeType: "test.business." + kind, NodeVersion: "1.0.0", Config: json.RawMessage(`{}`), Position: &workflowgraph.Position{X: 260}})
	}
	nodes = append(nodes, workflowGraphNode{NodeInstanceID: "end", NodeType: "core.end", NodeVersion: "1.0.0", Config: json.RawMessage(`{}`), Position: &workflowgraph.Position{X: 520}})
	g := workflowGraph{SchemaVersion: 3, EntryPoints: map[string]string{"main": "manual"}, Nodes: nodes}
	for i := 1; i < len(nodes); i++ {
		g.Edges = append(g.Edges, workflowGraphEdge{EdgeID: fmt.Sprint("e", i), SourceNodeInstanceID: nodes[i-1].NodeInstanceID, SourcePort: "out", TargetNodeInstanceID: nodes[i].NodeInstanceID, TargetPort: "in"})
	}
	return g
}

func createTestWorkflow(t *testing.T, a *App, ctx context.Context, p *Principal, g workflowGraph) WorkflowDetail {
	t.Helper()
	w, err := a.CreateWorkflow(ctx, WorkflowCreatePayload{Name: "Synthetic", Graph: mustJSON(g), MaxConcurrentRuns: 4}, p)
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func queueTestRun(t *testing.T, a *App, ctx context.Context, p *Principal, w WorkflowDetail) WorkflowRunView {
	t.Helper()
	r, err := a.CreateWorkflowRun(ctx, w.ID, WorkflowRunCreatePayload{RevisionID: w.DraftRevisionID}, p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func claimTestRun(t *testing.T, a *App) db.WorkflowRun {
	t.Helper()
	r, ok, err := a.claimWorkflowRun(context.Background(), time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("claim: %v, %v", ok, err)
	}
	return r
}
func countTestRows(t *testing.T, a *App, table, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := a.DB.Table(table).Where(where, args...).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDraftPublicationAndAtomicConflicts(t *testing.T) {
	a, ctx, p := platformFixture(t, nil)
	g := testGraph("")
	w := createTestWorkflow(t, a, ctx, p, g)
	first := w.DraftRevisionID
	if _, err := a.PublishWorkflowRevision(ctx, w.ID, WorkflowPublishPayload{RevisionID: first}, p); err != nil {
		t.Fatal(err)
	}
	queueTestRun(t, a, ctx, p, w)
	for i := 0; i < 12; i++ {
		r, err := a.SaveWorkflowRevision(ctx, w.ID, WorkflowRevisionSavePayload{ExpectedDraftRevisionID: w.DraftRevisionID, Graph: mustJSON(g)}, p)
		if err != nil {
			t.Fatal(err)
		}
		w.DraftRevisionID = r.ID
	}
	latest, err := a.GetWorkflow(ctx, w.ID)
	if err != nil || latest.PublishedRevisionID != first {
		t.Fatal("saving changed publication", err)
	}
	if countTestRows(t, a, "workflow_revisions", "id=?", first) != 1 {
		t.Fatal("referenced oldest revision was pruned")
	}
	before := countTestRows(t, a, "workflow_revisions", "workflow_id=?", w.ID)
	_, err = a.SaveWorkflowRevision(ctx, w.ID, WorkflowRevisionSavePayload{ExpectedDraftRevisionID: w.DraftRevisionID, Graph: mustJSON(g), Metadata: &WorkflowUpdatePayload{Name: ""}}, p)
	if err == nil || countTestRows(t, a, "workflow_revisions", "workflow_id=?", w.ID) != before {
		t.Fatal("invalid atomic metadata left a revision")
	}
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := a.SaveWorkflowRevision(ctx, w.ID, WorkflowRevisionSavePayload{ExpectedDraftRevisionID: w.DraftRevisionID, Graph: mustJSON(g)}, p)
			outcomes <- err
		}()
	}
	close(start)
	x, y := <-outcomes, <-outcomes
	if !((x == nil && errors.Is(y, ErrConflict)) || (y == nil && errors.Is(x, ErrConflict))) {
		t.Fatalf("parallel saves: %v %v", x, y)
	}
	if _, err := a.PublishWorkflowRevision(ctx, w.ID, WorkflowPublishPayload{RevisionID: first, ExpectedPublishedRevisionID: 0}, p); !errors.Is(err, ErrConflict) {
		t.Fatal("stale publish was accepted", err)
	}
}

func TestLeaseFencingAndRecoveryPolicies(t *testing.T) {
	for _, kind := range []string{"task", "external"} {
		t.Run(kind, func(t *testing.T) {
			a, ctx, p := platformFixture(t, nil)
			w := createTestWorkflow(t, a, ctx, p, testGraph(kind))
			queueTestRun(t, a, ctx, p, w)
			run := claimTestRun(t, a)
			var revision db.WorkflowRevision
			if err := a.DB.First(&revision, run.RevisionID).Error; err != nil {
				t.Fatal(err)
			}
			node := testGraph(kind).Nodes[1]
			now := time.Now().UTC()
			rn := db.WorkflowRunNode{RunID: run.ID, NodeInstanceID: node.NodeInstanceID, NodeType: node.NodeType, NodeVersion: node.NodeVersion, Status: RunStatusRunning, ExecutionPool: "stream", Attempt: 1, OperationKey: workflowOperationKey(run.ID, node.NodeInstanceID, 0), InputSummary: "{}", OutputSummary: "{}", StartedAt: now, InvocationStarted: true}
			if err := a.DB.Create(&rn).Error; err != nil {
				t.Fatal(err)
			}
			if err := a.DB.Model(&run).Update("lease_expires_at", now.Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}
			if err := a.commitWorkflowNodeSuccess(ctx, rn, run, revision, node, rn.OperationKey, 0, json.RawMessage(`{}`), json.RawMessage(`{"value":1}`), nil, now); err == nil {
				t.Fatal("expired executor committed")
			}
			if err := a.recoverExpiredRuns(ctx); err != nil {
				t.Fatal(err)
			}
			var recovered db.WorkflowRun
			if err := a.DB.First(&recovered, run.ID).Error; err != nil {
				t.Fatal(err)
			}
			if kind == "external" {
				if recovered.Status != RunStatusFailed || recovered.ErrorCategory == nil || *recovered.ErrorCategory != string(sdk.ErrorUnknownResult) {
					t.Fatal("unsafe invocation was repeated")
				}
				if _, err := a.ApplyWorkflowRunAction(ctx, run.ID, WorkflowRunActionPayload{Action: "retry"}); !errors.Is(err, ErrConflict) {
					t.Fatal("unknown effect was retried", err)
				}
			} else {
				if recovered.Status != RunStatusQueued {
					t.Fatal("pure computation was not recovered")
				}
				next := claimTestRun(t, a)
				if next.LeaseToken == nil || *next.LeaseToken == *run.LeaseToken {
					t.Fatal("recovery reused a lease")
				}
				if err := a.commitWorkflowNodeSuccess(ctx, rn, run, revision, node, rn.OperationKey, 0, json.RawMessage(`{}`), nil, nil, now); err == nil {
					t.Fatal("replaced executor committed")
				}
			}
			if countTestRows(t, a, "workflow_run_checkpoints", "run_id=?", run.ID) != 0 || countTestRows(t, a, "workflow_node_states", "workflow_id=?", w.ID) != 0 {
				t.Fatal("stale execution left durable facts")
			}
		})
	}
}

func TestStateIsSerialAcrossPartitionsAndIsolatedByRevision(t *testing.T) {
	a, ctx, p := platformFixture(t, func(ctx context.Context, r sdk.ActionRequest) (sdk.ActionResult, error) {
		raw, err := r.State.Load(ctx)
		if err != nil {
			return sdk.ActionResult{}, err
		}
		var state struct {
			Count int `json:"count"`
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &state); err != nil {
				return sdk.ActionResult{}, err
			}
		}
		state.Count++
		if err := r.State.Save(ctx, mustJSON(state)); err != nil {
			return sdk.ActionResult{}, err
		}
		return sdk.ActionResult{Output: json.RawMessage(`{}`)}, nil
	})
	g := testGraph("state")
	w := createTestWorkflow(t, a, ctx, p, g)
	for _, partition := range []string{"a", "b"} {
		r := queueTestRun(t, a, ctx, p, w)
		if err := a.DB.Model(&db.WorkflowRun{}).Where("id=?", r.ID).Update("partition_key", partition).Error; err != nil {
			t.Fatal(err)
		}
	}
	first := claimTestRun(t, a)
	if _, ok, err := a.claimWorkflowRun(ctx, time.Now().UTC()); err != nil || ok {
		t.Fatal("stateful partitions overlapped", err)
	}
	a.executeWorkflowRun(ctx, first)
	a.executeWorkflowRun(ctx, claimTestRun(t, a))
	var old db.WorkflowNodeState
	if err := a.DB.Where("workflow_id=?", w.ID).First(&old).Error; err != nil {
		t.Fatal(err)
	}
	if old.StateJSON != `{"count": 2}` {
		var value map[string]int
		if json.Unmarshal([]byte(old.StateJSON), &value) != nil || value["count"] != 2 {
			t.Fatal("state was not carried", old.StateJSON)
		}
	}
	r, err := a.SaveWorkflowRevision(ctx, w.ID, WorkflowRevisionSavePayload{ExpectedDraftRevisionID: w.DraftRevisionID, Graph: mustJSON(g)}, p)
	if err != nil {
		t.Fatal(err)
	}
	w.DraftRevisionID = r.ID
	queueTestRun(t, a, ctx, p, w)
	a.executeWorkflowRun(ctx, claimTestRun(t, a))
	var state db.WorkflowNodeState
	if err := a.DB.Where("workflow_id=? AND revision_id=?", w.ID, r.ID).First(&state).Error; err != nil {
		t.Fatal(err)
	}
	var value map[string]int
	if json.Unmarshal([]byte(state.StateJSON), &value) != nil || value["count"] != 1 {
		t.Fatal("new revision reused old state")
	}
}

func TestApprovalDecisionSupersessionExpiryAndCancel(t *testing.T) {
	a, ctx, p := platformFixture(t, nil)
	g := testGraph("")
	approval := workflowGraphNode{NodeInstanceID: "approval", NodeType: "core.human_approval", NodeVersion: "1.0.0", Config: json.RawMessage(`{"decisionMode":"human","taskType":"review","prompt":"Synthetic","expiresSeconds":60}`), Position: &workflowgraph.Position{X: 260}, InputBindings: map[string]workflowInputBinding{"businessKey": {Kind: "literal", Value: mustJSON("synthetic-key")}}}
	g.Nodes = append(g.Nodes[:1], approval, g.Nodes[1])
	g.Edges = []workflowGraphEdge{{EdgeID: "ma", SourceNodeInstanceID: "manual", SourcePort: "out", TargetNodeInstanceID: "approval", TargetPort: "in"}, {EdgeID: "ae", SourceNodeInstanceID: "approval", SourcePort: "out", TargetNodeInstanceID: "end", TargetPort: "in"}}
	w := createTestWorkflow(t, a, ctx, p, g)
	makeTask := func() db.WorkflowHumanTask {
		queueTestRun(t, a, ctx, p, w)
		run := claimTestRun(t, a)
		a.executeWorkflowRun(ctx, run)
		var task db.WorkflowHumanTask
		if err := a.DB.Where("run_id=?", run.ID).First(&task).Error; err != nil {
			t.Fatal(err)
		}
		var status string
		if err := a.DB.Model(&db.WorkflowRun{}).Where("id=?", run.ID).Pluck("status", &status).Error; err != nil || status != RunStatusWaiting {
			t.Fatal("visible pending task has no durable wait", err)
		}
		return task
	}
	one := makeTask()
	if _, err := a.DecideWorkflowHumanTask(ctx, one.ID, WorkflowHumanTaskDecision{Action: "approve"}, p); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DecideWorkflowHumanTask(ctx, one.ID, WorkflowHumanTaskDecision{Action: "reject"}, p); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate decision succeeded", err)
	}
	a.executeWorkflowRun(ctx, claimTestRun(t, a))
	two := makeTask()
	three := makeTask()
	var old db.WorkflowHumanTask
	if err := a.DB.First(&old, two.ID).Error; err != nil || old.Status != "superseded" {
		t.Fatal("new business task did not supersede old wait", err)
	}
	if _, err := a.ApplyWorkflowRunAction(ctx, three.RunID, WorkflowRunActionPayload{Action: "cancel"}); err != nil {
		t.Fatal(err)
	}
	old = db.WorkflowHumanTask{}
	if err := a.DB.First(&old, three.ID).Error; err != nil {
		t.Fatal(err)
	}
	if old.Status != "superseded" {
		t.Fatal("cancel left a pending task")
	}
	if countTestRows(t, a, "plugin_references", "reference_type='run' AND reference_id=? AND active", strconv.FormatInt(three.RunID, 10)) != 0 {
		t.Fatal("cancel left active run references")
	}
	a.executeWorkflowRun(ctx, claimTestRun(t, a))
	expired := makeTask()
	if err := a.DB.Model(&expired).Update("expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.expireWorkflowHumanTasks(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DecideWorkflowHumanTask(ctx, expired.ID, WorkflowHumanTaskDecision{Action: "approve"}, p); !errors.Is(err, ErrConflict) {
		t.Fatal("expired task was approved", err)
	}
}

func TestNonFinancialPluginNodeScopeAndSessionRevocation(t *testing.T) {
	a, ctx, p := platformFixture(t, nil)
	w := createTestWorkflow(t, a, ctx, p, testGraph("task"))
	if _, err := a.ResolveWorkflowScope(ctx, "test.business", w.ID, "task"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ResolveWorkflowScope(ctx, "test.business", w.ID, "end"); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign node accepted", err)
	}
	reader := *p
	reader.AccessTokenID = "synthetic-session"
	reader.AccessTokenExp = time.Now().UTC().Add(time.Hour)
	if err := a.LogoutAccessToken(&reader); err != nil {
		t.Fatal(err)
	}
	restarted := NewApp(a.DB, a.Cfg, a.Plugins)
	if _, err := restarted.RevalidateSession(&reader); err == nil {
		t.Fatal("logout vanished after restart")
	}
	if err := a.DB.Transaction(func(tx *gorm.DB) error { return restarted.ValidateSession(WithPrincipal(ctx, &reader), tx) }); err == nil {
		t.Fatal("revoked session committed")
	}
}

func TestResourceScopeArtifactAndSessionRevalidation(t *testing.T) {
	a, ctx, owner := platformFixture(t, func(ctx context.Context, request sdk.ActionRequest) (sdk.ActionResult, error) {
		artifact, err := request.Artifacts.Put(ctx, "text/plain", strings.NewReader("synthetic business artifact"))
		return sdk.ActionResult{Output: json.RawMessage(`{}`), Artifacts: []sdk.Artifact{artifact}}, err
	})
	w := createTestWorkflow(t, a, ctx, owner, testGraph("task"))
	run := queueTestRun(t, a, ctx, owner, w)
	a.executeWorkflowRun(ctx, claimTestRun(t, a))
	detail, err := a.GetWorkflowRunDetail(ctx, run.ID)
	if err != nil || detail.Status != RunStatusSucceeded || len(detail.Artifacts) != 1 {
		t.Fatal("synthetic artifact execution failed", err)
	}
	if _, err := a.GetWorkflowArtifactManifest(ctx, detail.Artifacts[0].SHA256, true); err != nil {
		t.Fatal("owner could not verify its artifact", err)
	}
	if _, err := a.GetWorkflowArtifactManifest(ctx, strings.Repeat("0", 64), false); !errors.Is(err, ErrNotFound) {
		t.Fatal("unreferenced artifact was exposed", err)
	}
	reader := *owner
	reader.AccessTokenID, reader.AccessTokenExp = "synthetic-session", time.Now().UTC().Add(time.Hour)
	readCtx := WithPrincipal(context.Background(), &reader)
	scope, err := a.ResolveSystemScope(readCtx, "test.business")
	if err != nil || !scope.AllWorkflows {
		t.Fatal("personal scope lost its workflows", err)
	}
	expired := reader
	expired.AccessTokenExp = time.Now().UTC().Add(-time.Second)
	if _, err := a.RevalidateSession(&expired); err == nil {
		t.Fatal("expired session remained valid")
	}
	if err := a.DB.Exec("UPDATE users SET session_version=session_version+1 WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if err := scope.SessionValid(context.Background()); err == nil {
		t.Fatal("long-lived scope survived password invalidation")
	}
	if _, err := principalForTx(a.DB, &Principal{User: &db.SystemUser{ID: 1}}); err != nil {
		t.Fatal("internal worker lost its owner", err)
	}

}

func TestLoopParentsDoNotExhaustChildPool(t *testing.T) {
	var entered atomic.Int32
	barrier := make(chan struct{})
	a, ctx, p := platformFixture(t, func(ctx context.Context, _ sdk.ActionRequest) (sdk.ActionResult, error) {
		if entered.Add(1) == 4 {
			close(barrier)
		}
		select {
		case <-barrier:
			return sdk.ActionResult{Output: json.RawMessage(`{}`)}, nil
		case <-ctx.Done():
			return sdk.ActionResult{}, ctx.Err()
		}
	})
	g := testLoopGraph(20)
	runs := []db.WorkflowRun{}
	for i := 0; i < 4; i++ {
		w := createTestWorkflow(t, a, ctx, p, g)
		queueTestRun(t, a, ctx, p, w)
		runs = append(runs, claimTestRun(t, a))
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	for _, run := range runs {
		workers.Add(1)
		go func(run db.WorkflowRun) { defer workers.Done(); a.executeWorkflowRun(runCtx, run) }(run)
	}
	workers.Wait()
	if entered.Load() != 4 {
		t.Fatal("Loop parents held all child slots")
	}
	for _, run := range runs {
		var stored db.WorkflowRun
		if err := a.DB.First(&stored, run.ID).Error; err != nil || stored.Status != RunStatusSucceeded {
			t.Fatal("Loop failed", stored.Status, err)
		}
	}
	if len(a.streamSlots) != 0 {
		t.Fatal("Loop left occupied slots")
	}
}

func TestPasswordChangeInvalidatesSessionsAndRollsBack(t *testing.T) {
	a, ctx, owner := platformFixture(t, nil)
	oldHash := a.Hasher.HashPassword("synthetic-old")
	if err := a.DB.Exec("UPDATE users SET password_hash=? WHERE id=1", oldHash).Error; err != nil {
		t.Fatal(err)
	}
	firstToken := a.Tokens.CreateAccessToken(1, 1, false).Value
	secondToken := a.Tokens.CreateAccessToken(1, 1, false).Value
	first, err := a.AuthenticateAccessToken(firstToken)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.AuthenticateAccessToken(secondToken)
	if err != nil {
		t.Fatal(err)
	}
	reauth, err := a.Reauthenticate(first, "synthetic-old")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ChangePassword(ctx, first, "wrong", "synthetic-new"); err == nil {
		t.Fatal("wrong current password accepted")
	}
	if err := a.DB.Exec(`CREATE FUNCTION synthetic_fail_password() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic password failure'; END $$; CREATE TRIGGER synthetic_password_failure BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION synthetic_fail_password()`).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.ChangePassword(ctx, first, "synthetic-old", "synthetic-new"); err == nil {
		t.Fatal("injected update failure ignored")
	}
	var user db.SystemUser
	if err := a.DB.First(&user, 1).Error; err != nil {
		t.Fatal(err)
	}
	if user.PasswordHash != oldHash || user.SessionVersion != 1 {
		t.Fatal("password failure partially committed")
	}
	if err := a.DB.Exec("DROP TRIGGER synthetic_password_failure ON users").Error; err != nil {
		t.Fatal(err)
	}
	if err := a.ChangePassword(ctx, first, "synthetic-old", "synthetic-new"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{firstToken, secondToken} {
		if _, err := a.AuthenticateAccessToken(token); err == nil {
			t.Fatal("old session survived password change")
		}
	}
	if a.ConsumeReauthToken(reauth, first) {
		t.Fatal("old secondary confirmation survived password change")
	}
	if _, err := commandPrincipalTx(a.DB, WithPrincipal(ctx, second)); err == nil {
		t.Fatal("old session could commit a command")
	}
	if _, err := a.Login("coinsphere", "synthetic-old", false); err == nil {
		t.Fatal("old password accepted")
	}
	if _, err := a.Login("coinsphere", "synthetic-new", false); err != nil {
		t.Fatal(err)
	}
	if owner.User.ID != user.ID {
		t.Fatal("password change replaced account identity")
	}
}

func TestErrorClassesAndDiagnosticReplaySuppressEffects(t *testing.T) {
	for _, kind := range []string{"task", "external"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			a, ctx, p := platformFixture(t, func(context.Context, sdk.ActionRequest) (sdk.ActionResult, error) {
				calls.Add(1)
				return sdk.ActionResult{}, &sdk.ExecutionError{Class: sdk.ErrorTransient, Err: errors.New("synthetic temporary failure")}
			})
			w := createTestWorkflow(t, a, ctx, p, testGraph(kind))
			r := queueTestRun(t, a, ctx, p, w)
			a.executeWorkflowRun(ctx, claimTestRun(t, a))
			var stored db.WorkflowRun
			if err := a.DB.First(&stored, r.ID).Error; err != nil {
				t.Fatal(err)
			}
			if kind == "task" {
				if stored.Status != RunStatusRetrying {
					t.Fatal("safe transient computation was not retried", stored.Status)
				}
			} else {
				if stored.Status != RunStatusFailed || stored.ErrorCategory == nil || *stored.ErrorCategory != string(sdk.ErrorUnknownResult) {
					t.Fatal("external temporary failure lost unknown outcome classification")
				}
				replay, err := a.ApplyWorkflowRunAction(ctx, r.ID, WorkflowRunActionPayload{Action: "replay"})
				if err != nil {
					t.Fatal(err)
				}
				a.executeWorkflowRun(ctx, claimTestRun(t, a))
				if calls.Load() != 1 {
					t.Fatal("diagnostic replay issued a new external call")
				}
				var diagnostic db.WorkflowRun
				if err := a.DB.First(&diagnostic, replay.ID).Error; err != nil || diagnostic.Status != RunStatusFailed {
					t.Fatal("missing side-effect checkpoint was silently invented", err)
				}
			}
		})
	}
}

func TestLeaseRenewalFailureRejectsExecutorAndRunBudgetIsBounded(t *testing.T) {
	entered := make(chan struct{}, 4)
	barrier := make(chan struct{})
	a, ctx, p := platformFixture(t, func(ctx context.Context, _ sdk.ActionRequest) (sdk.ActionResult, error) {
		entered <- struct{}{}
		select {
		case <-barrier:
			return sdk.ActionResult{Output: json.RawMessage(`{}`)}, nil
		case <-ctx.Done():
			return sdk.ActionResult{}, ctx.Err()
		}
	})
	w := createTestWorkflow(t, a, ctx, p, testGraph("task"))
	queueTestRun(t, a, ctx, p, w)
	stale := claimTestRun(t, a)
	executorCtx, stopExecutor := context.WithTimeout(ctx, 15*time.Second)
	defer stopExecutor()
	executorDone := make(chan struct{})
	go func() { a.executeWorkflowRun(executorCtx, stale); close(executorDone) }()
	select {
	case <-entered:
	case <-executorCtx.Done():
		t.Fatal("executor did not enter the blocking action")
	}
	if err := a.DB.Model(&db.WorkflowRun{}).Where("id=?", stale.ID).Update("lease_token", "replacement").Error; err != nil {
		t.Fatal(err)
	}
	select {
	case <-executorDone:
	case <-executorCtx.Done():
		t.Fatal("failed periodic renewal did not terminate the executor")
	}
	if countTestRows(t, a, "workflow_run_checkpoints", "run_id=? AND node_instance_id='task'", stale.ID) != 0 || len(a.streamSlots) != 0 {
		t.Fatal("lost executor committed output or retained a slot")
	}
	if a.renewWorkflowRunLease(ctx, stale) == nil {
		t.Fatal("zero-row renewal was accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if a.renewWorkflowRunLease(cancelled, stale) == nil {
		t.Fatal("renewal database failure was accepted")
	}
	if err := a.recoverExpiredRuns(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.DB.Model(&db.WorkflowRun{}).Where("id=?", stale.ID).Updates(map[string]any{"status": RunStatusCancelled, "lease_token": nil, "lease_expires_at": nil, "completed_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		queueTestRun(t, a, ctx, p, w)
	}
	a.runSlots = make(chan struct{}, 2)
	engineCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- a.RunWorkflowEngine(engineCtx) }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-engineCtx.Done():
			t.Fatal("engine did not fill its bounded budget")
		}
	}
	if countTestRows(t, a, "workflow_runs", "status=?", RunStatusRunning) != 2 || countTestRows(t, a, "workflow_runs", "status=?", RunStatusQueued) != 2 {
		t.Fatal("engine claimed beyond the Run budget")
	}
	close(barrier)
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine failed to stop")
	}
	if err := a.WaitForWorkflowRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(a.runSlots) != 0 || len(a.streamSlots) != 0 {
		t.Fatal("cancelled engine retained execution slots")
	}
}

func testLoopGraph(timeout int) workflowGraph {
	body := workflowGraph{SchemaVersion: 3, Nodes: []workflowGraphNode{
		{NodeInstanceID: "item", NodeType: "core.loop_item", NodeVersion: "1.0.0", Config: json.RawMessage(`{}`), Position: &workflowgraph.Position{}},
		{NodeInstanceID: "work", NodeType: "test.business.task", NodeVersion: "1.0.0", Config: json.RawMessage(`{}`), Position: &workflowgraph.Position{X: 260}},
		{NodeInstanceID: "done", NodeType: "core.loop_end", NodeVersion: "1.0.0", Config: json.RawMessage(`{}`), Position: &workflowgraph.Position{X: 520}, InputBindings: map[string]workflowInputBinding{"value": {Kind: "field", NodeInstanceID: "item", FieldPath: []string{"value"}}}},
	}, Edges: []workflowGraphEdge{
		{EdgeID: "iw", SourceNodeInstanceID: "item", SourcePort: "out", TargetNodeInstanceID: "work", TargetPort: "in"},
		{EdgeID: "we", SourceNodeInstanceID: "work", SourcePort: "out", TargetNodeInstanceID: "done", TargetPort: "in"},
	}}
	g := testGraph("task")
	g.Nodes[1].NodeType = "core.loop"
	g.Nodes[1].Config = mustJSON(workflowLoopConfig{MaxIterations: 1, TimeoutSeconds: timeout, ExitCondition: "input.iteration >= 1", Body: body})
	return g
}

func TestApprovalVisibilityAndConcurrentDecisions(t *testing.T) {
	a, ctx, p := platformFixture(t, nil)
	g := testGraph("task")
	g.Nodes[1] = workflowGraphNode{NodeInstanceID: "task", NodeType: "core.human_approval", NodeVersion: "1.0.0", Config: json.RawMessage(`{"decisionMode":"human","taskType":"review","prompt":"Synthetic","expiresSeconds":60}`), Position: &workflowgraph.Position{X: 260}, InputBindings: map[string]workflowInputBinding{"businessKey": {Kind: "literal", Value: mustJSON("barrier-case")}}}
	w := createTestWorkflow(t, a, ctx, p, g)
	queueTestRun(t, a, ctx, p, w)
	run := claimTestRun(t, a)
	inserted := make(chan int64, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	executorCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Pause after INSERT, while the task and durable waiting state still share
	// the production transaction. Another connection must see neither yet.
	if err := a.DB.Callback().Create().After("gorm:create").Register("test:approval_barrier", func(tx *gorm.DB) {
		if tx.Statement.Table != "workflow_human_tasks" {
			return
		}
		task, ok := tx.Statement.Dest.(*db.WorkflowHumanTask)
		if !ok {
			return
		}
		inserted <- task.ID
		select {
		case <-release:
		case <-executorCtx.Done():
			tx.AddError(executorCtx.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer a.DB.Callback().Create().Remove("test:approval_barrier")
	done := make(chan struct{})
	go func() { a.executeWorkflowRun(executorCtx, run); close(done) }()
	var taskID int64
	select {
	case taskID = <-inserted:
	case <-executorCtx.Done():
		t.Fatal("approval did not reach the transaction barrier")
	}
	if tasks, err := a.PageWorkflowHumanTasks(ctx, WorkflowHumanTaskQuery{Status: "pending", Page: CursorPage{Limit: 50}}); err != nil || len(tasks["records"].([]WorkflowHumanTaskView)) != 0 {
		t.Fatal("uncommitted task became visible", err)
	}
	if _, err := a.DecideWorkflowHumanTask(ctx, taskID, WorkflowHumanTaskDecision{Action: "approve"}, p); !errors.Is(err, ErrNotFound) {
		t.Fatal("decision saw a task before durable wait", err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-done:
	case <-executorCtx.Done():
		t.Fatal("approval failed to enter durable wait")
	}
	start := make(chan struct{})
	decisions := make(chan error, 2)
	for _, action := range []string{"approve", "reject"} {
		go func(action string) {
			<-start
			_, err := a.DecideWorkflowHumanTask(ctx, taskID, WorkflowHumanTaskDecision{Action: action}, p)
			decisions <- err
		}(action)
	}
	close(start)
	successes, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		select {
		case err := <-decisions:
			if err == nil {
				successes++
			} else if errors.Is(err, ErrConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		case <-executorCtx.Done():
			t.Fatal("concurrent decisions deadlocked")
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("decisions were not exactly once")
	}
	a.executeWorkflowRun(ctx, claimTestRun(t, a))
	var stored db.WorkflowRun
	if err := a.DB.First(&stored, run.ID).Error; err != nil || stored.Status != RunStatusSucceeded {
		t.Fatal("decided task left a waiting run", err)
	}
}

func TestLoopAbsoluteDeadlineAndCancellationReleaseSlots(t *testing.T) {
	for _, mode := range []string{"absolute", "timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			a, ctx, p := platformFixture(t, func(ctx context.Context, _ sdk.ActionRequest) (sdk.ActionResult, error) {
				entered <- struct{}{}
				<-ctx.Done()
				return sdk.ActionResult{}, ctx.Err()
			})
			g := testLoopGraph(1)
			w := createTestWorkflow(t, a, ctx, p, g)
			queueTestRun(t, a, ctx, p, w)
			run := claimTestRun(t, a)
			if mode == "absolute" {
				var revision db.WorkflowRevision
				if err := a.DB.First(&revision, run.RevisionID).Error; err != nil {
					t.Fatal(err)
				}
				for i, started := range []time.Time{time.Now().UTC().Add(-2 * time.Second), time.Now().UTC()} {
					node := db.WorkflowRunNode{RunID: run.ID, NodeInstanceID: "task", NodeType: "core.loop", NodeVersion: "1.0.0", Status: RunStatusRunning, ExecutionPool: "stream", Attempt: i + 1, OperationKey: workflowOperationKey(run.ID, "task", 0), InputSummary: "{}", OutputSummary: "{}", StartedAt: started}
					if err := a.DB.Create(&node).Error; err != nil {
						t.Fatal(err)
					}
				}
				_, err := a.executeWorkflowLoop(ctx, run, revision, g.Nodes[1], json.RawMessage(`{}`), nil)
				if err == nil || !strings.Contains(err.Error(), "总超时时间") || len(entered) != 0 {
					t.Fatal("retry reset the absolute Loop deadline", err)
				}
				return
			}
			executorCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			done := make(chan struct{})
			go func() { a.executeWorkflowRun(executorCtx, run); close(done) }()
			select {
			case <-entered:
			case <-executorCtx.Done():
				t.Fatal("loop child did not enter")
			}
			if mode == "cancel" {
				if _, err := a.ApplyWorkflowRunAction(ctx, run.ID, WorkflowRunActionPayload{Action: "cancel"}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-done:
			case <-executorCtx.Done():
				t.Fatal("loop failed to terminate")
			}
			var stored db.WorkflowRun
			if err := a.DB.First(&stored, run.ID).Error; err != nil {
				t.Fatal(err)
			}
			want := RunStatusFailed
			if mode == "cancel" {
				want = RunStatusCancelled
			}
			if stored.Status != want || len(a.streamSlots) != 0 || len(a.computeSlots) != 0 {
				t.Fatal("loop retained resources or wrong terminal state", stored.Status)
			}
		})
	}
}

func TestWorkflowRunAndHumanTaskPages(t *testing.T) {
	a, ctx, p := platformFixture(t, nil)
	first := createTestWorkflow(t, a, ctx, p, testGraph("task"))
	second := createTestWorkflow(t, a, ctx, p, testGraph("task"))
	firstRun := queueTestRun(t, a, ctx, p, first)
	secondRun := queueTestRun(t, a, ctx, p, second)
	page, err := a.PageWorkflowRuns(ctx, 0, WorkflowRunListQuery{Page: CursorPage{Limit: 1, scope: "all-runs"}})
	if err != nil {
		t.Fatal(err)
	}
	records := page["records"].([]WorkflowRunView)
	if len(records) != 1 || records[0].ID != secondRun.ID || records[0].WorkflowName != second.Name || records[0].RevisionNumber != 1 || page["total"].(int64) != 2 || !page["hasMore"].(bool) {
		t.Fatalf("unexpected run page: %#v", page)
	}
	next, err := ParseCursorPage(page["nextCursor"].(string), 1, "all-runs")
	if err != nil {
		t.Fatal(err)
	}
	tail, err := a.PageWorkflowRuns(ctx, 0, WorkflowRunListQuery{Page: next})
	if err != nil || tail["hasMore"].(bool) || tail["records"].([]WorkflowRunView)[0].ID != firstRun.ID {
		t.Fatalf("run cursor: %#v %v", tail, err)
	}
	if _, err := ParseCursorPage(page["nextCursor"].(string), 1, "filtered-runs"); err == nil {
		t.Fatal("cursor accepted under different filters")
	}
	filtered, err := a.PageWorkflowRuns(ctx, first.ID, WorkflowRunListQuery{Page: CursorPage{Limit: 20}})
	if err != nil || filtered["total"].(int64) != 1 || filtered["records"].([]WorkflowRunView)[0].ID != firstRun.ID {
		t.Fatalf("workflow filter: %#v %v", filtered, err)
	}
	if _, err := a.PageWorkflowRuns(context.Background(), 0, WorkflowRunListQuery{Page: CursorPage{Limit: 20}}); !errors.Is(err, ErrPermission) {
		t.Fatal("anonymous run list accepted", err)
	}
	now := time.Now().UTC()
	for _, run := range []WorkflowRunView{firstRun, secondRun} {
		task := db.WorkflowHumanTask{WorkflowID: run.WorkflowID, RunID: run.ID, NodeInstanceID: "task", TaskType: "review", BusinessKey: fmt.Sprintf("page-%d", run.ID), Prompt: "分页测试", Status: "pending", DecisionJSON: "{}", ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
		if err := a.DB.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := a.PageWorkflowHumanTasks(ctx, WorkflowHumanTaskQuery{Status: "pending", Page: CursorPage{Limit: 1, scope: "pending"}})
	if err != nil {
		t.Fatal(err)
	}
	taskRows := tasks["records"].([]WorkflowHumanTaskView)
	if len(taskRows) != 1 || tasks["total"].(int64) != 2 || !tasks["hasMore"].(bool) || taskRows[0].RunID != secondRun.ID || taskRows[0].WorkflowName != second.Name || taskRows[0].NodeName != "task" {
		t.Fatalf("unexpected task page: %#v", tasks)
	}
	nextTask, err := ParseCursorPage(tasks["nextCursor"].(string), 1, "pending")
	if err != nil {
		t.Fatal(err)
	}
	taskTail, err := a.PageWorkflowHumanTasks(ctx, WorkflowHumanTaskQuery{Status: "pending", Page: nextTask})
	if err != nil || taskTail["hasMore"].(bool) || taskTail["records"].([]WorkflowHumanTaskView)[0].RunID != firstRun.ID {
		t.Fatalf("task cursor: %#v %v", taskTail, err)
	}
	scoped, err := a.PageWorkflowHumanTasks(ctx, WorkflowHumanTaskQuery{WorkflowID: first.ID, RunID: firstRun.ID, Page: CursorPage{Limit: 20}})
	if err != nil || scoped["total"].(int64) != 1 {
		t.Fatalf("task filters: %#v %v", scoped, err)
	}
	if _, err := a.PageWorkflowHumanTasks(ctx, WorkflowHumanTaskQuery{Status: "invalid", Page: CursorPage{Limit: 20}}); err == nil {
		t.Fatal("invalid task status accepted")
	}
	if _, err := a.PageWorkflowHumanTasks(context.Background(), WorkflowHumanTaskQuery{Page: CursorPage{Limit: 20}}); !errors.Is(err, ErrPermission) {
		t.Fatal("anonymous task list accepted", err)
	}
}
