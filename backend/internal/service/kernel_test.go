package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"coinsphere/backend/internal/config"
	"coinsphere/backend/internal/db"
	"coinsphere/backend/internal/migration"
	"coinsphere/backend/internal/testdb"
	"coinsphere/backend/plugin/sdk"
	workflowgraph "coinsphere/backend/workflow/graph"
)

func TestKernelWithoutBusinessPlugins(t *testing.T) {
	sqlDB, database := testdb.Open(t, false)
	runner, err := migration.New(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	a := NewApp(database, &config.AppConfig{Auth: config.AuthConfig{SecretKey: "synthetic-kernel-key", PasswordIterations: 1, AccessTokenTTLMinutes: 60}}, sdk.NewRegistry())
	a.ArtifactRoot = t.TempDir()
	if err := db.Seed(context.Background(), database, a.Hasher, "synthetic-password", nil); err != nil {
		t.Fatal(err)
	}
	session, err := a.Login("coinsphere", "synthetic-password", false)
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.AuthenticateAccessToken(session.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithPrincipal(context.Background(), p)
	menus, err := a.GetMenuTree(p)
	if err != nil || len(menus) == 0 {
		t.Fatal("kernel navigation unavailable", err)
	}
	for _, node := range a.WorkflowNodeCatalog() {
		if a.Plugins.NodePlugin(node.Type) != "" {
			t.Fatal("business node in kernel")
		}
	}
	graph := testGraph("")
	approval := workflowGraphNode{NodeInstanceID: "approval", NodeType: "core.human_approval", NodeVersion: "1.0.0", Config: json.RawMessage(`{"decisionMode":"human","taskType":"review","prompt":"Synthetic","expiresSeconds":60}`), Position: &workflowgraph.Position{X: 260}, InputBindings: map[string]workflowInputBinding{"businessKey": {Kind: "literal", Value: mustJSON("kernel")}}}
	graph.Nodes = append(graph.Nodes[:1], approval, graph.Nodes[1])
	graph.Edges = []workflowGraphEdge{{EdgeID: "approval", SourceNodeInstanceID: "manual", SourcePort: "out", TargetNodeInstanceID: "approval", TargetPort: "in"}, {EdgeID: "end", SourceNodeInstanceID: "approval", SourcePort: "out", TargetNodeInstanceID: "end", TargetPort: "in"}}
	workflow := createTestWorkflow(t, a, ctx, p, graph)
	queued := queueTestRun(t, a, ctx, p, workflow)
	a.executeWorkflowRun(ctx, claimTestRun(t, a))
	var task db.WorkflowHumanTask
	if err := database.Where("run_id=?", queued.ID).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := a.DecideWorkflowHumanTask(ctx, task.ID, WorkflowHumanTaskDecision{Action: "approve"}, p); err != nil {
		t.Fatal(err)
	}
	a.executeWorkflowRun(ctx, claimTestRun(t, a))
	var run db.WorkflowRun
	if err := database.First(&run, queued.ID).Error; err != nil || run.Status != RunStatusSucceeded {
		t.Fatal("core approval run did not finish", err)
	}
	scheduled := testGraph("")
	scheduled.Nodes[0].NodeType = "core.schedule"
	scheduled.Nodes[0].Config = json.RawMessage(`{"everySeconds":60}`)
	scheduledWorkflow := createTestWorkflow(t, a, ctx, p, scheduled)
	if _, err := a.PublishWorkflowRevision(ctx, scheduledWorkflow.ID, WorkflowPublishPayload{RevisionID: scheduledWorkflow.DraftRevisionID}, p); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyWorkflowLifecycle(ctx, scheduledWorkflow.ID, WorkflowLifecyclePayload{Action: "activate"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := database.Model(&db.WorkflowRuntime{}).Where("workflow_id=?", scheduledWorkflow.ID).Update("next_scheduled_at", now.Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.enqueueScheduledRuns(ctx, now); err != nil {
		t.Fatal(err)
	}
	a.executeWorkflowRun(ctx, claimTestRun(t, a))
	if countTestRows(t, a, "workflow_runs", "workflow_id=? AND status='succeeded'", scheduledWorkflow.ID) != 1 {
		t.Fatal("kernel schedule did not run")
	}
}
