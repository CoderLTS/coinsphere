package service

import (
	"encoding/json"
	"testing"

	"coinsphere/backend/internal/db"
	workflowgraph "coinsphere/backend/workflow/graph"
)

func TestConditionOrdinaryRunAndLabels(t *testing.T) {
	a, ctx, p := platformFixture(t, nil)
	for _, expected := range []string{"true", "false", "unavailable"} {
		g := testGraph("")
		condition := workflowGraphNode{NodeInstanceID: "condition", Label: "价格判断", NodeType: "core.condition", NodeVersion: "1.0.0", Config: json.RawMessage(`{"match":"all","rules":[{"fieldPath":["price"],"operator":"gt","value":"9007199254740992.0000000001"}]}`), Position: &workflowgraph.Position{X: 260}, InputBindings: map[string]workflowgraph.Binding{}}
		if expected != "unavailable" {
			value := `"9007199254740992.0000000002"`
			if expected == "false" {
				value = `"9007199254740992"`
			}
			condition.InputBindings["price"] = workflowgraph.Binding{Kind: "literal", Value: json.RawMessage(value)}
		}
		g.Nodes = append(g.Nodes[:1], condition, g.Nodes[1])
		g.Edges = []workflowGraphEdge{{EdgeID: "start", SourceNodeInstanceID: "manual", SourcePort: "out", TargetNodeInstanceID: "condition", TargetPort: "in"}}
		for _, branch := range []string{"true", "false", "unavailable"} {
			g.Edges = append(g.Edges, workflowGraphEdge{EdgeID: branch, Label: branch, SourceNodeInstanceID: "condition", SourcePort: branch, TargetNodeInstanceID: "end", TargetPort: "in"})
		}
		w := createTestWorkflow(t, a, ctx, p, g)
		run := queueTestRun(t, a, ctx, p, w)
		a.executeWorkflowRun(ctx, claimTestRun(t, a))
		var node db.WorkflowRunNode
		if err := a.DB.Where("run_id=? AND node_instance_id=?", run.ID, "condition").First(&node).Error; err != nil {
			t.Fatal(err)
		}
		var output map[string]any
		if json.Unmarshal([]byte(node.OutputSummary), &output) != nil || output["branch"] != expected || node.Status != RunStatusSucceeded {
			t.Fatal(node.Status, output)
		}
		var revision db.WorkflowRevision
		if a.DB.First(&revision, w.DraftRevisionID).Error != nil {
			t.Fatal("missing revision")
		}
		var stored workflowGraph
		if json.Unmarshal([]byte(revision.GraphJSON), &stored) != nil || stored.Nodes[1].Label != condition.Label || stored.Edges[1].Label != "true" {
			t.Fatal("display labels did not roundtrip")
		}
	}
}
