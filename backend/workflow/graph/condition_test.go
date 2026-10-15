package graph

import (
	"encoding/json"
	"testing"
)

func TestConditionBranchesAndDecimal(t *testing.T) {
	for _, test := range []struct{ name, config, input, branch string }{
		{"decimal", `{"match":"all","rules":[{"fieldPath":["price"],"operator":"gt","value":"9007199254740992.000000000000000001"}]}`, `{"price":"9007199254740992.000000000000000002"}`, "true"},
		{"false", `{"match":"all","rules":[{"fieldPath":["value"],"operator":"true"}]}`, `{"value":false}`, "false"},
		{"missing", `{"match":"any","rules":[{"fieldPath":["value"],"operator":"true"},{"fieldPath":["missing"],"operator":"exists"}]}`, `{"value":true}`, "unavailable"},
		{"null", `{"match":"all","rules":[{"fieldPath":["value"],"operator":"true"},{"fieldPath":["missing"],"operator":"exists"}]}`, `{"value":false,"missing":null}`, "unavailable"},
		{"type", `{"match":"any","rules":[{"fieldPath":["value"],"operator":"true"},{"fieldPath":["text"],"operator":"eq","value":true}]}`, `{"value":true,"text":"true"}`, "unavailable"},
		{"any", `{"match":"any","rules":[{"fieldPath":["a"],"operator":"true"},{"fieldPath":["b"],"operator":"false"}]}`, `{"a":false,"b":false}`, "true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var input map[string]any
			_ = json.Unmarshal([]byte(test.input), &input)
			output, err := EvaluateCondition(json.RawMessage(test.config), input)
			if err != nil || output["branch"] != test.branch {
				t.Fatal(output, err)
			}
			for _, branch := range []string{"true", "false", "unavailable"} {
				got, err := EdgeReached(Edge{SourceNodeInstanceID: "condition", SourcePort: branch}, Context{Nodes: map[string]map[string]any{"condition": output}})
				if err != nil || got != (branch == test.branch) {
					t.Fatal("incorrect branch routing", branch, err)
				}
			}
		})
	}
	for _, config := range []string{`{}`, `{"match":"all","rules":[]}`, `{"match":"all","rules":[{"fieldPath":["x"],"operator":"eq"}]}`} {
		if ValidateCondition(json.RawMessage(config)) == nil {
			t.Fatal("invalid condition accepted")
		}
	}
}

func TestConditionResolveUnavailableAndExactNumber(t *testing.T) {
	node := Node{NodeType: "core.condition", InputBindings: map[string]Binding{"value": {Kind: "literal", Value: json.RawMessage(`9007199254740993`)}, "missing": {Kind: "field", NodeInstanceID: "upstream", FieldPath: []string{"absent"}}}}
	input, err := Resolve(node, nil, Context{})
	if err != nil || input["value"] != json.Number("9007199254740993") {
		t.Fatal(input, err)
	}
	node.NodeType = "other"
	if _, err := Resolve(node, nil, Context{}); err == nil {
		t.Fatal("missing binding outside condition accepted")
	}
}
