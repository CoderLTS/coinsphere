package quant

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"coinsphere/backend/plugin/sdk"
)

type quantWorkflowGraph struct {
	SchemaVersion int               `json:"schemaVersion"`
	EntryPoints   map[string]string `json:"entryPoints"`
	Nodes         []struct {
		ID            string                          `json:"nodeInstanceId"`
		Type          string                          `json:"nodeType"`
		Config        json.RawMessage                 `json:"config"`
		InputBindings map[string]quantWorkflowBinding `json:"inputBindings"`
	} `json:"nodes"`
	Edges []struct {
		Source, Port, Target string
	} `json:"-"`
	RawEdges []struct {
		Source string `json:"sourceNodeInstanceId"`
		Port   string `json:"sourcePort"`
		Target string `json:"targetNodeInstanceId"`
	} `json:"edges"`
}

type quantWorkflowBinding struct {
	Kind           string   `json:"kind"`
	NodeInstanceID string   `json:"nodeInstanceId"`
	FieldPath      []string `json:"fieldPath"`
}

func validateQuantWorkflow(input sdk.WorkflowValidationContext) error {
	var graph quantWorkflowGraph
	if json.Unmarshal(input.Graph, &graph) != nil {
		return errors.New("量化工作流定义无效")
	}
	nodes := make(map[string]struct {
		Type   string
		Config json.RawMessage
	}, len(graph.Nodes))
	quantNodes := 0
	for _, node := range graph.Nodes {
		nodes[node.ID] = struct {
			Type   string
			Config json.RawMessage
		}{node.Type, node.Config}
		if strings.HasPrefix(node.Type, quantPluginID+".") {
			quantNodes++
		}
	}
	if quantNodes == 0 {
		return nil
	}

	if err := validateQuantBindings(graph, nodes); err != nil {
		return err
	}
	if err := validateQuantSeriesIdentity(graph, nodes); err != nil {
		return err
	}
	for _, node := range graph.Nodes {
		if node.Type != "official.quant.output_signal" {
			continue
		}
		incoming := 0
		for _, edge := range graph.RawEdges {
			if edge.Target != node.ID {
				continue
			}
			incoming++
			if nodes[edge.Source].Type != "official.quant.position" {
				return fmt.Errorf("量化信号输出节点 %q 只接受仓位候选", node.ID)
			}
		}
		if incoming == 0 {
			return fmt.Errorf("量化信号输出节点 %q 缺少仓位候选", node.ID)
		}
	}
	return nil
}

func validateQuantBindings(graph quantWorkflowGraph, nodes map[string]struct {
	Type   string
	Config json.RawMessage
}) error {
	for _, node := range graph.Nodes {
		if node.Type == "official.quant.market_signal" {
			var incoming []struct{ Source, Port, Target string }
			for _, edge := range graph.RawEdges {
				if edge.Target == node.ID {
					incoming = append(incoming, struct{ Source, Port, Target string }{edge.Source, edge.Port, edge.Target})
				}
			}
			if len(incoming) != 1 || incoming[0].Port != "true" || !strings.HasPrefix(nodes[incoming[0].Source].Type, quantPluginID+".") {
				return fmt.Errorf("节点 %q 须连接且仅连接一个量化条件的成立分支", node.ID)
			}
			if err := validateFieldBindings(node.ID, incoming[0].Source, node.InputBindings, map[string]string{
				"venue": "venue", "market": "market", "instrument": "instrument", "interval": "interval",
				"name": "formula", "indicator": "indicator", "candleCloseTime": "candleCloseTime", "summary": "summary", "values": "value",
			}); err != nil {
				return err
			}
		}
		if node.Type == "official.quant.position" {
			var config struct{ TargetMode, DecimalField string }
			if json.Unmarshal(node.Config, &config) != nil || config.TargetMode != "input" {
				continue
			}
			binding, ok := node.InputBindings["target"]
			if !ok || binding.Kind != "field" || len(binding.FieldPath) != 2 || binding.FieldPath[0] != "decimals" ||
				binding.FieldPath[1] != config.DecimalField || nodes[binding.NodeInstanceID].Type != "official.quant.code_strategy" {
				return fmt.Errorf("节点 %q 的目标输入须引用所配置代码策略的 Decimal 输出", node.ID)
			}
		}
		for field, binding := range node.InputBindings {
			source := nodes[binding.NodeInstanceID]
			if binding.Kind != "field" || source.Type != "official.quant.code_strategy" || len(binding.FieldPath) != 2 ||
				binding.FieldPath[0] != "booleans" && binding.FieldPath[0] != "decimals" {
				continue
			}
			var config struct {
				BooleanOutputs []string `json:"booleanOutputs"`
				DecimalOutputs []string `json:"decimalOutputs"`
			}
			if json.Unmarshal(source.Config, &config) != nil {
				return fmt.Errorf("节点 %q 的输入绑定 %q 引用了无效代码策略", node.ID, field)
			}
			declared := config.BooleanOutputs
			if binding.FieldPath[0] == "decimals" {
				declared = config.DecimalOutputs
			}
			if !containsQuantString(declared, binding.FieldPath[1]) {
				return fmt.Errorf("节点 %q 的输入绑定 %q 引用了未声明的代码策略输出", node.ID, field)
			}
		}
	}
	return nil
}

func validateFieldBindings(nodeID, sourceID string, bindings map[string]quantWorkflowBinding, expected map[string]string) error {
	for targetField, sourceField := range expected {
		binding, ok := bindings[targetField]
		if !ok || binding.Kind != "field" || binding.NodeInstanceID != sourceID || len(binding.FieldPath) != 1 || binding.FieldPath[0] != sourceField {
			return fmt.Errorf("节点 %q 的输入绑定 %q 须引用来源量化条件", nodeID, targetField)
		}
	}
	return nil
}

func validateQuantSeriesIdentity(graph quantWorkflowGraph, nodes map[string]struct {
	Type   string
	Config json.RawMessage
}) error {
	for _, driver := range graph.Nodes {
		if driver.Type != "official.quant.backtest_start" {
			continue
		}
		var main struct{ Venue, Market, Instrument, Interval string }
		if json.Unmarshal(driver.Config, &main) != nil {
			return errors.New("量化回测行情序列无效")
		}
		queue := []string{}
		for _, edge := range graph.RawEdges {
			if edge.Source == driver.ID && edge.Port == "each" {
				queue = append(queue, edge.Target)
			}
		}
		seen := map[string]bool{}
		outputs := 0
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if seen[id] {
				continue
			}
			seen[id] = true
			node := nodes[id]
			if node.Type == "official.quant.output_signal" {
				var output struct{ Venue, Market, Instrument, Interval string }
				if json.Unmarshal(node.Config, &output) != nil || output != main {
					return fmt.Errorf("节点 %q 须使用回测驱动节点 %q 的行情序列", id, driver.ID)
				}
				outputs++
				continue
			}
			for _, edge := range graph.RawEdges {
				if edge.Source == id {
					queue = append(queue, edge.Target)
				}
			}
		}
		if outputs == 0 {
			return fmt.Errorf("回测驱动节点 %q 须通过逐项分支到达信号输出节点", driver.ID)
		}
	}

	for _, node := range graph.Nodes {
		if node.Type != "official.quant.output_signal" {
			continue
		}
		var output struct{ Venue, Market, Instrument, Interval string }
		if json.Unmarshal(node.Config, &output) != nil {
			return fmt.Errorf("节点 %q 的信号输出标识无效", node.ID)
		}

		incoming := 0
		for _, edge := range graph.RawEdges {
			if edge.Target != node.ID {
				continue
			}
			incoming++
			if nodes[edge.Source].Type != "official.quant.position" {
				return fmt.Errorf("节点 %q 只接受量化仓位候选", node.ID)
			}
		}
		if incoming == 0 {
			return fmt.Errorf("节点 %q 缺少量化仓位候选", node.ID)
		}
	}
	return nil
}
