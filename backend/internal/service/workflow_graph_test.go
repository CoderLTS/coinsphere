package service

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestWorkflowValidationIssueTargets(t *testing.T) {
	a := &App{}
	g := testGraph("")
	g.Nodes[1].Label = string(make([]rune, 81))
	err := a.ValidateWorkflowGraph(mustJSON(g))
	var issue *WorkflowValidationIssue
	if !errors.As(err, &issue) || issue.Code != "node.label_length" || issue.NodeInstanceID != "end" || len(issue.FieldPath) != 1 || issue.FieldPath[0] != "label" {
		t.Fatalf("missing node field target: %#v %v", issue, err)
	}
	g = testGraph("")
	g.Edges[0].SourcePort = "missing"
	err = a.ValidateWorkflowGraph(mustJSON(g))
	if !errors.As(err, &issue) || issue.Code != "edge.port" || issue.EdgeID != g.Edges[0].EdgeID {
		t.Fatalf("missing edge target: %#v %v", issue, err)
	}
	g = testLoopGraph(1)
	var config workflowLoopConfig
	if err := json.Unmarshal(g.Nodes[1].Config, &config); err != nil {
		t.Fatal(err)
	}
	config.Body.Nodes = append(config.Body.Nodes[:1], config.Body.Nodes[2:]...)
	config.Body.Edges = []workflowGraphEdge{{EdgeID: "item-done", SourceNodeInstanceID: "item", SourcePort: "out", TargetNodeInstanceID: "done", TargetPort: "in"}}
	config.Body.Nodes[1].Label = string(make([]rune, 81))
	g.Nodes[1].Config = mustJSON(config)
	err = a.ValidateWorkflowGraph(mustJSON(g))
	if !errors.As(err, &issue) || issue.Code != "node.label_length" || issue.NodeInstanceID != "task."+config.Body.Nodes[1].NodeInstanceID {
		t.Fatalf("missing loop node target: %#v %v", issue, err)
	}
}

func TestWorkflowSchemaFieldBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		path   []string
		valid  bool
		typ    string
	}{
		{"dynamic model output", `{"type":"object","properties":{"data":{"type":"object"}},"additionalProperties":false}`, []string{"data", "message"}, true, ""},
		{"nested dynamic output", `{"type":"object"}`, []string{"data", "message"}, true, ""},
		{"explicit dynamic output", `{"type":"object","additionalProperties":true}`, []string{"message"}, true, ""},
		{"typed map", `{"type":"object","additionalProperties":{"type":"string"}}`, []string{"message"}, true, "string"},
		{"closed object", `{"type":"object","additionalProperties":false}`, []string{"message"}, false, ""},
		{"scalar has no fields", `{"type":"string"}`, []string{"message"}, false, ""},
		{"typed map values have no fields", `{"type":"object","additionalProperties":{"type":"string"}}`, []string{"message", "value"}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field, valid := workflowSchemaField(json.RawMessage(tc.schema), tc.path)
			if valid != tc.valid || tc.typ != "" && field["type"] != tc.typ {
				t.Fatalf("field=%v valid=%v", field, valid)
			}
		})
	}
}
