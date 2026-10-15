package graph

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestNamespacesMergeAndLoopExpressions(t *testing.T) {
	ctx := Context{Input: map[string]any{"key": "values"}, Nodes: map[string]map[string]any{"a": {"value": "left", "values": []any{"first"}}, "b": {"value": "right"}}}
	node := Node{InputBindings: map[string]Binding{"a": {Kind: "field", NodeInstanceID: "a", FieldPath: []string{"value"}}, "b": {Kind: "cel", Expression: `nodes["b"].value`}}}
	got, err := Resolve(node, nil, ctx)
	if err != nil || got["a"] != "left" || got["b"] != "right" {
		t.Fatalf("namespaced inputs were not preserved: %v %v", got, err)
	}
	expression := `nodes["a"][input.key][0] == "first"`
	if err := ValidateNodeReferences(expression, map[string]bool{"a": true}); err != nil {
		t.Fatal(err)
	}
	rewritten, err := RewriteNodeReferences(expression, map[string]string{"a": "loop.a"})
	if err != nil {
		t.Fatal(err)
	}
	ctx.Nodes["loop.a"] = ctx.Nodes["a"]
	delete(ctx.Nodes, "a")
	value, err := Evaluate(rewritten, ctx)
	if err != nil || value != true {
		t.Fatalf("Loop field access changed: %v %v", value, err)
	}
	for _, invalid := range []string{`nodes[input.key].value`, `nodes`, `nodes["unavailable"].value`} {
		if ValidateNodeReferences(invalid, map[string]bool{"a": true}) == nil {
			t.Fatalf("accepted unavailable node reference %s", invalid)
		}
	}

}

func TestExplicitReadinessAndSingleJoin(t *testing.T) {
	g := Graph{Nodes: []Node{{NodeInstanceID: "root"}, {NodeInstanceID: "a"}, {NodeInstanceID: "b"}, {NodeInstanceID: "join"}}, Edges: []Edge{{SourceNodeInstanceID: "root", TargetNodeInstanceID: "a", SourcePort: "out"}, {SourceNodeInstanceID: "root", TargetNodeInstanceID: "b", SourcePort: "out"}, {SourceNodeInstanceID: "a", TargetNodeInstanceID: "join", SourcePort: "out"}, {SourceNodeInstanceID: "b", TargetNodeInstanceID: "join", SourcePort: "out"}}}
	order, _, err := Order(g, "root")
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 4 || order[len(order)-1] != "join" {
		t.Fatalf("join must be visited once after upstreams: %v", order)
	}
	ctx := Context{Nodes: map[string]map[string]any{"a": {"ready": false}}}
	edge := Edge{SourceNodeInstanceID: "a", SourcePort: "out"}
	if reached, err := EdgeReached(edge, ctx); err != nil || !reached {
		t.Fatal("Core must not infer business readiness")
	}
	edge.Condition = `nodes["a"].ready`
	if reached, err := EdgeReached(edge, ctx); err != nil || reached {
		t.Fatal("explicit readiness condition was ignored")
	}
}

func TestDecimalGuardAndConcurrentCompilation(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"price":{"type":"string","x-coinsphere-decimal":true},"count":{"type":"integer"}}}`)
	schemas := map[string]json.RawMessage{"nodes.money": schema, "input": schema}
	for _, expr := range []string{`double(nodes.money.price) * 2.0`, `double(input["price"]) + 1.0`, `nodes.money.price + input.price`, `"price: " + string(double(nodes.money.price) + 1.0)`} {
		if !HasDecimalArithmetic(expr, schemas) {
			t.Fatal("allowed generic Decimal arithmetic", expr)
		}
	}
	if HasDecimalArithmetic(`nodes.money.count + 1`, schemas) {
		t.Fatal("ordinary integer arithmetic was blocked")
	}
	for expr, expected := range map[string]string{`"price: " + nodes.money.price`: "price: 123.45", `input.price + " USD"`: "123.45 USD"} {
		if HasDecimalArithmetic(expr, schemas) {
			t.Fatal("Decimal string presentation was blocked", expr)
		}
		value, err := Evaluate(expr, Context{Input: map[string]any{"price": "123.45"}, Nodes: map[string]map[string]any{"money": {"price": "123.45"}}})
		if err != nil || value != expected {
			t.Fatal("Decimal text changed", value, err)
		}
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 20 {
				value, err := Evaluate(`input.count + 1`, Context{Input: map[string]any{"count": 1}})
				if err != nil || value != int64(2) {
					t.Errorf("concurrent CEL evaluation: %v %v", value, err)
				}
			}
		})
	}
	wg.Wait()
}
