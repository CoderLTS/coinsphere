package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/operators"
	"coinsphere/backend/plugin/sdk"
	workflowgraph "coinsphere/backend/workflow/graph"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

const (
	maxWorkflowNodes    = 256
	maxWorkflowEdges    = 1024
	maxWorkflowCELBytes = 4096
)

var workflowNodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type validatedWorkflowGraph struct {
	graphJSON        string
	nodeVersionsJSON string
	mainTriggerID    string
	entryPoints      map[string]string
	nodes            map[string]workflowGraphNode
	nodeTypes        map[string]string
	nodeVersions     map[string]workflowNodeVersion
	descriptors      map[string]sdk.NodeDescriptor
	requiredSecrets  map[workflowSecretKey]bool
}

type workflowNodeVersion struct {
	NodeType    string `json:"nodeType"`
	NodeVersion string `json:"nodeVersion"`
}

type workflowGraph = workflowgraph.Graph
type workflowGraphNode = workflowgraph.Node
type workflowGraphEdge = workflowgraph.Edge
type workflowInputBinding = workflowgraph.Binding

type workflowLoopConfig struct {
	MaxIterations  int           `json:"maxIterations"`
	TimeoutSeconds int           `json:"timeoutSeconds"`
	ExitCondition  string        `json:"exitCondition"`
	Body           workflowGraph `json:"body"`
}

type validatedWorkflowLoop struct {
	config          workflowLoopConfig
	itemID          string
	endID           string
	nodes           map[string]workflowGraphNode
	descriptors     map[string]sdk.NodeDescriptor
	requiredSecrets map[workflowSecretKey]bool
}

type WorkflowValidationIssue struct {
	Scope          string   `json:"scope"`
	Level          string   `json:"level"`
	Code           string   `json:"code"`
	Message        string   `json:"message"`
	NodeInstanceID string   `json:"nodeInstanceId,omitempty"`
	EdgeID         string   `json:"edgeId,omitempty"`
	FieldPath      []string `json:"fieldPath,omitempty"`
}

func (issue *WorkflowValidationIssue) Error() string { return issue.Message }
func workflowIssue(code, message, node, edge string, field ...string) *WorkflowValidationIssue {
	scope := "graph"
	if node != "" {
		scope = "node"
	} else if edge != "" {
		scope = "edge"
	}
	return &WorkflowValidationIssue{Scope: scope, Level: "error", Code: code, Message: message, NodeInstanceID: node, EdgeID: edge, FieldPath: field}
}

func (a *App) ValidateWorkflowGraph(raw json.RawMessage) error {
	_, err := a.validateWorkflowGraph(raw)
	return err
}

func (a *App) validateWorkflowGraph(raw json.RawMessage) (validatedWorkflowGraph, error) {
	if len(raw) == 0 || len(raw) > maxWorkflowGraphBytes {
		return validatedWorkflowGraph{}, fmt.Errorf("工作流定义大小必须在 1 至 %d 字节之间", maxWorkflowGraphBytes)
	}
	var graph workflowGraph
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&graph); err != nil {
		return validatedWorkflowGraph{}, errors.New("工作流定义必须符合 Graph 3 格式")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return validatedWorkflowGraph{}, errors.New("工作流定义必须是单个 JSON 对象")
	}
	if graph.SchemaVersion != 3 {
		return validatedWorkflowGraph{}, errors.New("工作流 schemaVersion 必须为 3")
	}
	if len(graph.Nodes) == 0 || len(graph.Nodes) > maxWorkflowNodes {
		return validatedWorkflowGraph{}, fmt.Errorf("工作流节点数量必须在 1 至 %d 之间", maxWorkflowNodes)
	}
	if len(graph.Edges) > maxWorkflowEdges {
		return validatedWorkflowGraph{}, fmt.Errorf("工作流连线数量不能超过 %d", maxWorkflowEdges)
	}

	descriptorCatalog := a.workflowNodeDescriptors()
	nodes := make(map[string]workflowGraphNode, len(graph.Nodes))
	nodeTypes := make(map[string]string, len(graph.Nodes))
	descriptors := make(map[string]sdk.NodeDescriptor, len(graph.Nodes))
	versions := make(map[string]workflowNodeVersion, len(graph.Nodes))
	requiredSecrets := make(map[workflowSecretKey]bool)
	triggerID := ""
	for _, node := range graph.Nodes {
		if len([]rune(node.Label)) > 80 {
			return validatedWorkflowGraph{}, workflowIssue("node.label_length", "节点名称不能超过 80 个字符", node.NodeInstanceID, "", "label")
		}
		if !workflowNodeIDPattern.MatchString(node.NodeInstanceID) || nodes[node.NodeInstanceID].NodeInstanceID != "" {
			return validatedWorkflowGraph{}, workflowIssue("node.id", "节点标识无效或重复", node.NodeInstanceID, "", "nodeInstanceId")
		}
		desc, ok := descriptorCatalog[node.NodeType]
		if !ok {
			return validatedWorkflowGraph{}, workflowIssue("node.unavailable", "节点类型不可用，请检查插件加载状态", node.NodeInstanceID, "", "nodeType")
		}
		if desc.Version != node.NodeVersion {
			return validatedWorkflowGraph{}, workflowIssue("node.version", "节点版本与当前插件不一致", node.NodeInstanceID, "", "nodeVersion")
		}
		if node.NodeType == "core.loop_item" || node.NodeType == "core.loop_end" {
			return validatedWorkflowGraph{}, workflowIssue("node.loop_only", "此节点只能用于循环子图", node.NodeInstanceID, "", "nodeType")
		}
		if desc.Kind == sdk.NodeKindTrigger {
			if triggerID != "" {
				return validatedWorkflowGraph{}, errors.New("工作流必须包含且仅包含一个主触发节点")
			}
			triggerID = node.NodeInstanceID
		}
		if node.Position == nil {
			return validatedWorkflowGraph{}, workflowIssue("node.position", "请为节点设置画布位置", node.NodeInstanceID, "", "position")
		}
		config, err := decodeJSONObject(node.Config)
		if err != nil {
			return validatedWorkflowGraph{}, workflowIssue("node.config_object", "节点配置必须是 JSON 对象", node.NodeInstanceID, "", "config")
		}
		secretFields, secretRequired := workflowSecretFields(desc.ConfigSchema)
		for field := range secretFields {
			if _, exists := config[field]; exists {
				return validatedWorkflowGraph{}, workflowIssue("node.secret_in_graph", "凭据必须通过加密凭据字段保存", node.NodeInstanceID, "", "config", field)
			}
			if secretRequired[field] {
				requiredSecrets[workflowSecretKey{node.NodeInstanceID, field}] = true
			}
		}
		ordinarySchema, err := ordinaryWorkflowConfigSchema(desc.ConfigSchema, secretFields)
		if err != nil || validateWorkflowSchemaValue(ordinarySchema, config) != nil {
			return validatedWorkflowGraph{}, workflowIssue("node.config_schema", "节点配置不符合字段要求，请检查必填项和取值范围", node.NodeInstanceID, "", "config")
		}
		if desc.ValidateConfig != nil {
			if err := desc.ValidateConfig(node.Config); err != nil {
				return validatedWorkflowGraph{}, workflowIssue("node.config", "节点配置无效："+err.Error(), node.NodeInstanceID, "", "config")
			}
		}
		if node.NodeType == "core.schedule" {
			if _, err := nextWorkflowScheduledAt(node.Config, time.Now().UTC()); err != nil {
				return validatedWorkflowGraph{}, workflowIssue("node.schedule", "定时配置无效，请检查 Cron 表达式、间隔与时区", node.NodeInstanceID, "", "config")
			}
		}
		nodes[node.NodeInstanceID] = node
		nodeTypes[node.NodeInstanceID] = node.NodeType
		descriptors[node.NodeInstanceID] = desc
		versions[node.NodeInstanceID] = workflowNodeVersion{NodeType: node.NodeType, NodeVersion: node.NodeVersion}
	}
	if triggerID == "" {
		return validatedWorkflowGraph{}, errors.New("工作流必须包含且仅包含一个主触发节点")
	}
	entryPoints := graph.EntryPoints
	if len(entryPoints) == 0 || entryPoints["main"] != triggerID {
		return validatedWorkflowGraph{}, errors.New("主入口必须指向主触发节点")
	}
	for key, id := range entryPoints {
		if !workflowNodeIDPattern.MatchString(key) || len(key) > 32 || nodes[id].NodeInstanceID == "" {
			return validatedWorkflowGraph{}, fmt.Errorf("工作流入口 %q 无效", key)
		}
	}

	for _, node := range graph.Nodes {
		if node.NodeType != "core.loop" {
			continue
		}
		loop, err := validateWorkflowLoop(node, descriptorCatalog)
		if err != nil {
			var issue *WorkflowValidationIssue
			if errors.As(err, &issue) {
				qualified := *issue
				if qualified.NodeInstanceID != "" {
					qualified.NodeInstanceID = node.NodeInstanceID + "." + qualified.NodeInstanceID
				}
				if qualified.EdgeID != "" {
					qualified.EdgeID = node.NodeInstanceID + "." + qualified.EdgeID
				}
				return validatedWorkflowGraph{}, &qualified
			}
			return validatedWorkflowGraph{}, workflowIssue("node.loop_body", err.Error(), node.NodeInstanceID, "", "config", "body")
		}
		for bodyID, bodyNode := range loop.nodes {
			compositeID, err := workflowLoopNodeID(node.NodeInstanceID, bodyID)
			if err != nil {
				return validatedWorkflowGraph{}, fmt.Errorf("节点 %q：%w", node.NodeInstanceID, err)
			}
			if _, exists := nodeTypes[compositeID]; exists {
				return validatedWorkflowGraph{}, fmt.Errorf("节点 %q 的循环子节点标识 %q 与其他节点冲突", node.NodeInstanceID, bodyID)
			}
			nodeTypes[compositeID] = bodyNode.NodeType
			descriptors[compositeID] = loop.descriptors[bodyID]
			versions[compositeID] = workflowNodeVersion{NodeType: bodyNode.NodeType, NodeVersion: bodyNode.NodeVersion}
			for key := range loop.requiredSecrets {
				if key.nodeInstanceID == bodyID {
					requiredSecrets[workflowSecretKey{compositeID, key.field}] = true
				}
			}
		}
	}
	adjacency, reverse, err := validateWorkflowEdges(graph.Edges, nodes, descriptors, triggerID)
	if err != nil {
		return validatedWorkflowGraph{}, err
	}
	roots := make([]string, 0, len(entryPoints))
	for _, root := range entryPoints {
		roots = append(roots, root)
	}
	for _, edge := range graph.Edges {
		for _, root := range roots {
			if edge.TargetNodeInstanceID == root {
				return validatedWorkflowGraph{}, workflowIssue("edge.entry_target", "连线不能指向工作流入口", "", edge.EdgeID, "targetNodeInstanceId")
			}
		}
	}

	if err := validateWorkflowTopology(nodes, adjacency, reverse, roots); err != nil {
		return validatedWorkflowGraph{}, err
	}
	rootSet := map[string]bool{}
	for _, root := range roots {
		rootSet[root] = true
	}
	if err := validateWorkflowBindings(nodes, descriptors, adjacency, graph.Edges, rootSet); err != nil {
		return validatedWorkflowGraph{}, err
	}

	if err := validateWorkflowExpressions(nodes, descriptors, adjacency, graph.Edges, rootSet); err != nil {
		return validatedWorkflowGraph{}, err
	}

	canonical, err := json.Marshal(graph)
	if err != nil {
		return validatedWorkflowGraph{}, errors.New("编码工作流定义失败")
	}
	nodeVersions, err := json.Marshal(versions)
	if err != nil {
		return validatedWorkflowGraph{}, errors.New("编码工作流节点版本失败")
	}
	if a.Plugins != nil {
		definitions := []json.RawMessage{canonical}
		for _, node := range graph.Nodes {
			if node.NodeType == "core.loop" {
				var config workflowLoopConfig
				if err := json.Unmarshal(node.Config, &config); err != nil {
					return validatedWorkflowGraph{}, err
				}
				raw, err := json.Marshal(config.Body)
				if err != nil {
					return validatedWorkflowGraph{}, err
				}
				definitions = append(definitions, raw)
			}
		}
		for _, validator := range a.Plugins.WorkflowValidators() {
			for _, definition := range definitions {
				if err := validator.ValidateWorkflow(sdk.WorkflowValidationContext{Graph: definition, Nodes: descriptorCatalog}); err != nil {
					return validatedWorkflowGraph{}, err
				}
			}
		}
	}
	return validatedWorkflowGraph{
		graphJSON: string(canonical), nodeVersionsJSON: string(nodeVersions), mainTriggerID: triggerID, entryPoints: entryPoints,
		nodes: nodes, nodeTypes: nodeTypes, nodeVersions: versions, descriptors: descriptors, requiredSecrets: requiredSecrets,
	}, nil
}

func validateWorkflowLoop(node workflowGraphNode, catalog map[string]sdk.NodeDescriptor) (validatedWorkflowLoop, error) {
	var config workflowLoopConfig
	decoder := json.NewDecoder(bytes.NewReader(node.Config))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || config.MaxIterations < 1 || config.MaxIterations > 100 ||
		config.TimeoutSeconds < 1 || config.TimeoutSeconds > 86400 {
		return validatedWorkflowLoop{}, errors.New("循环配置无效")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return validatedWorkflowLoop{}, errors.New("循环配置必须是单个 JSON 对象")
	}
	ast, err := compileWorkflowCEL(config.ExitCondition)
	if err != nil || ast.OutputType().TypeName() != "bool" {
		return validatedWorkflowLoop{}, errors.New("退出条件必须是返回布尔值的 CEL 表达式")
	}
	if config.Body.SchemaVersion != 3 || len(config.Body.Nodes) < 2 || len(config.Body.Nodes) > maxWorkflowNodes || len(config.Body.Edges) > maxWorkflowEdges {
		return validatedWorkflowLoop{}, errors.New("循环子图必须是符合 Graph 3 格式及大小限制的有向无环图")
	}

	nodes := make(map[string]workflowGraphNode, len(config.Body.Nodes))
	descriptors := make(map[string]sdk.NodeDescriptor, len(config.Body.Nodes))
	requiredSecrets := make(map[workflowSecretKey]bool)
	itemID, endID := "", ""
	for _, bodyNode := range config.Body.Nodes {
		if len([]rune(bodyNode.Label)) > 80 {
			return validatedWorkflowLoop{}, workflowIssue("node.label_length", "节点名称不能超过 80 个字符", bodyNode.NodeInstanceID, "", "label")
		}
		if !workflowNodeIDPattern.MatchString(bodyNode.NodeInstanceID) || nodes[bodyNode.NodeInstanceID].NodeInstanceID != "" {
			return validatedWorkflowLoop{}, workflowIssue("node.id", "循环子节点标识无效或重复", bodyNode.NodeInstanceID, "", "nodeInstanceId")
		}
		desc, ok := catalog[bodyNode.NodeType]
		if !ok || desc.Version != bodyNode.NodeVersion {
			return validatedWorkflowLoop{}, workflowIssue("node.unavailable", "循环子节点的类型或版本不可用", bodyNode.NodeInstanceID, "", "nodeType")
		}
		if desc.Kind == sdk.NodeKindTrigger || bodyNode.NodeType == "core.loop" || bodyNode.NodeType == "core.end" || desc.SideEffect == sdk.SideEffectHumanAction {
			return validatedWorkflowLoop{}, fmt.Errorf("节点 %q 不支持用于循环子图", bodyNode.NodeInstanceID)
		}
		if bodyNode.NodeType == "core.loop_item" {
			if itemID != "" {
				return validatedWorkflowLoop{}, errors.New("循环子图必须包含且仅包含一个循环项节点")
			}
			itemID = bodyNode.NodeInstanceID
		}
		if bodyNode.NodeType == "core.loop_end" {
			if endID != "" {
				return validatedWorkflowLoop{}, errors.New("循环子图必须包含且仅包含一个循环结束节点")
			}
			endID = bodyNode.NodeInstanceID
		}
		if bodyNode.Position == nil {
			return validatedWorkflowLoop{}, workflowIssue("node.position", "循环子节点缺少画布位置", bodyNode.NodeInstanceID, "", "position")
		}
		values, err := decodeJSONObject(bodyNode.Config)
		if err != nil {
			return validatedWorkflowLoop{}, workflowIssue("node.config_object", "循环子节点配置必须是 JSON 对象", bodyNode.NodeInstanceID, "", "config")
		}
		secretFields, secretRequired := workflowSecretFields(desc.ConfigSchema)
		for field := range secretFields {
			if _, exists := values[field]; exists {
				return validatedWorkflowLoop{}, workflowIssue("node.secret_in_graph", "凭据必须通过加密凭据字段保存", bodyNode.NodeInstanceID, "", "config", field)
			}
			if secretRequired[field] {
				requiredSecrets[workflowSecretKey{bodyNode.NodeInstanceID, field}] = true
			}
		}
		ordinarySchema, err := ordinaryWorkflowConfigSchema(desc.ConfigSchema, secretFields)
		if err != nil || validateWorkflowSchemaValue(ordinarySchema, values) != nil {
			return validatedWorkflowLoop{}, workflowIssue("node.config_schema", "循环子节点配置不符合字段要求", bodyNode.NodeInstanceID, "", "config")
		}
		if desc.ValidateConfig != nil {
			if err := desc.ValidateConfig(bodyNode.Config); err != nil {
				return validatedWorkflowLoop{}, workflowIssue("node.config", "循环子节点配置无效："+err.Error(), bodyNode.NodeInstanceID, "", "config")
			}
		}
		nodes[bodyNode.NodeInstanceID] = bodyNode
		descriptors[bodyNode.NodeInstanceID] = desc
	}
	if itemID == "" || endID == "" {
		return validatedWorkflowLoop{}, errors.New("循环子图必须包含且仅包含一个循环项节点和一个循环结束节点")
	}
	adjacency, reverse, err := validateWorkflowEdges(config.Body.Edges, nodes, descriptors, itemID)
	if err != nil {
		return validatedWorkflowLoop{}, err
	}
	if err := validateWorkflowTopology(nodes, adjacency, reverse, []string{itemID}); err != nil {
		return validatedWorkflowLoop{}, err
	}
	for id, next := range adjacency {
		if len(next) == 0 && id != endID {
			return validatedWorkflowLoop{}, fmt.Errorf("循环子节点 %q 必须连接到循环结束节点", id)
		}
	}
	if err := validateWorkflowBindings(nodes, descriptors, adjacency, config.Body.Edges, map[string]bool{itemID: true}); err != nil {
		return validatedWorkflowLoop{}, err
	}
	if err := validateWorkflowExpressions(nodes, descriptors, adjacency, config.Body.Edges, map[string]bool{itemID: true}); err != nil {
		return validatedWorkflowLoop{}, err
	}
	allowed := map[string]bool{}
	decimalSchemas := map[string]json.RawMessage{"input": descriptors[itemID].InputSchema}
	for id, d := range descriptors {
		allowed[id] = true
		decimalSchemas["nodes."+id] = d.OutputSchema
	}
	if err := workflowgraph.ValidateNodeReferences(config.ExitCondition, allowed); err != nil {
		return validatedWorkflowLoop{}, err
	}
	if workflowgraph.HasDecimalArithmetic(config.ExitCondition, decimalSchemas) {
		return validatedWorkflowLoop{}, errors.New("循环退出条件不能进行 Decimal 算术")
	}

	return validatedWorkflowLoop{
		config: config, itemID: itemID, endID: endID, nodes: nodes,
		descriptors: descriptors, requiredSecrets: requiredSecrets,
	}, nil
}

func workflowLoopNodeID(loopID, bodyID string) (string, error) {
	id := loopID + "." + bodyID
	if !workflowNodeIDPattern.MatchString(id) {
		return "", fmt.Errorf("循环子节点标识 %q 过长", bodyID)
	}
	return id, nil
}

func validateWorkflowEdges(edges []workflowGraphEdge, nodes map[string]workflowGraphNode, descriptors map[string]sdk.NodeDescriptor, triggerID string) (map[string][]string, map[string][]string, error) {
	adjacency := make(map[string][]string, len(nodes))
	reverse := make(map[string][]string, len(nodes))
	seenIDs := make(map[string]bool, len(edges))
	seenEdges := make(map[string]bool, len(edges))
	for id := range nodes {
		adjacency[id] = nil
		reverse[id] = nil
	}
	for _, edge := range edges {
		if len([]rune(edge.Label)) > 80 {
			return nil, nil, workflowIssue("edge.label_length", "连线名称不能超过 80 个字符", "", edge.EdgeID, "label")
		}
		if !workflowNodeIDPattern.MatchString(edge.EdgeID) || seenIDs[edge.EdgeID] {
			return nil, nil, workflowIssue("edge.id", "连线标识无效或重复", "", edge.EdgeID, "edgeId")
		}
		seenIDs[edge.EdgeID] = true
		if nodes[edge.SourceNodeInstanceID].NodeInstanceID == "" || nodes[edge.TargetNodeInstanceID].NodeInstanceID == "" {
			return nil, nil, workflowIssue("edge.node", "连线引用了不存在的节点", "", edge.EdgeID, "sourceNodeInstanceId")
		}
		if edge.SourceNodeInstanceID == edge.TargetNodeInstanceID {
			return nil, nil, workflowIssue("edge.self_loop", "连线不能指向同一节点", "", edge.EdgeID, "targetNodeInstanceId")
		}
		_, sourcePorts := workflowPorts(descriptors[edge.SourceNodeInstanceID])
		targetPorts, _ := workflowPorts(descriptors[edge.TargetNodeInstanceID])
		if !containsString(sourcePorts, edge.SourcePort) || !containsString(targetPorts, edge.TargetPort) {
			return nil, nil, workflowIssue("edge.port", "连线端口不可用", "", edge.EdgeID, "sourcePort")
		}
		if edge.TargetNodeInstanceID == triggerID {
			return nil, nil, workflowIssue("edge.trigger_target", "连线不能指向主触发节点", "", edge.EdgeID, "targetNodeInstanceId")
		}
		identity := strings.Join([]string{edge.SourceNodeInstanceID, edge.SourcePort, edge.TargetNodeInstanceID, edge.TargetPort}, "\x00")
		if seenEdges[identity] {
			return nil, nil, workflowIssue("edge.duplicate", "此连接已存在", "", edge.EdgeID, "sourceNodeInstanceId")
		}
		seenEdges[identity] = true
		if expression := strings.TrimSpace(edge.Condition); expression != "" {
			ast, err := compileWorkflowCEL(expression)
			if err != nil || ast.OutputType().TypeName() != "bool" {
				return nil, nil, workflowIssue("edge.condition", "连线条件必须是返回布尔值的 CEL 表达式", "", edge.EdgeID, "condition")
			}
		}
		adjacency[edge.SourceNodeInstanceID] = append(adjacency[edge.SourceNodeInstanceID], edge.TargetNodeInstanceID)
		reverse[edge.TargetNodeInstanceID] = append(reverse[edge.TargetNodeInstanceID], edge.SourceNodeInstanceID)
	}
	return adjacency, reverse, nil
}

func validateWorkflowTopology(nodes map[string]workflowGraphNode, adjacency, reverse map[string][]string, roots []string) error {
	degrees := make(map[string]int, len(nodes))
	for id := range nodes {
		degrees[id] = len(reverse[id])
	}
	queue := make([]string, 0, len(nodes))
	for id, degree := range degrees {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adjacency[id] {
			degrees[next]--
			if degrees[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(nodes) {
		return errors.New("工作流不能包含循环连线，请使用循环节点")
	}
	reachable := map[string]bool{}
	queue = append([]string(nil), roots...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if reachable[id] {
			continue
		}
		reachable[id] = true
		queue = append(queue, adjacency[id]...)
	}
	if len(reachable) != len(nodes) {
		return errors.New("存在无法从入口到达的节点")
	}
	return nil
}

func validateWorkflowBindings(nodes map[string]workflowGraphNode, descriptors map[string]sdk.NodeDescriptor, adjacency map[string][]string, edges []workflowGraphEdge, roots map[string]bool) error {
	for nodeID, node := range nodes {
		_, required := workflowSchemaProperties(descriptors[nodeID].InputSchema)
		if !roots[nodeID] {
			for field := range required {
				if _, ok := node.InputBindings[field]; !ok {
					return workflowIssue("binding.invalid", "缺少必填输入绑定", nodeID, "", "inputBindings", field)
				}
			}
		}
		for field, binding := range node.InputBindings {
			targetSchema, ok := workflowSchemaField(descriptors[nodeID].InputSchema, []string{field})
			if !ok {
				return workflowIssue("binding.invalid", "输入字段未在 Schema 中声明", nodeID, "", "inputBindings", field)
			}
			switch binding.Kind {
			case "field":
				if binding.NodeInstanceID == "" || len(binding.FieldPath) == 0 || len(binding.Value) != 0 || binding.Expression != "" {
					return workflowIssue("binding.invalid", "输出字段绑定格式无效", nodeID, "", "inputBindings", field)
				}
				if nodes[binding.NodeInstanceID].NodeInstanceID == "" || !workflowPathExists(adjacency, binding.NodeInstanceID, nodeID) {
					return workflowIssue("binding.invalid", "输入绑定必须引用上游节点", nodeID, "", "inputBindings", field)
				}
				sourceSchema, ok := workflowSchemaField(descriptors[binding.NodeInstanceID].OutputSchema, binding.FieldPath)
				if !ok || !workflowSchemaTypesCompatible(sourceSchema, targetSchema) {
					return workflowIssue("binding.invalid", "输出字段路径不存在或类型不兼容", nodeID, "", "inputBindings", field)
				}
			case "literal":
				if binding.NodeInstanceID != "" || len(binding.FieldPath) != 0 || len(binding.Value) == 0 || binding.Expression != "" {
					return workflowIssue("binding.invalid", "固定值绑定格式无效", nodeID, "", "inputBindings", field)
				}
				var value any
				if json.Unmarshal(binding.Value, &value) != nil || validateWorkflowSchemaValue(schemaFragment(targetSchema), value) != nil {
					return workflowIssue("binding.invalid", "固定值类型不符合字段要求", nodeID, "", "inputBindings", field)
				}
			case "cel":
				if binding.NodeInstanceID != "" || len(binding.FieldPath) != 0 || len(binding.Value) != 0 || strings.TrimSpace(binding.Expression) == "" {
					return workflowIssue("binding.invalid", "CEL 绑定格式无效", nodeID, "", "inputBindings", field)
				}
				ast, err := compileWorkflowCEL(binding.Expression)
				if err != nil || !workflowCELTypeCompatible(ast.OutputType().TypeName(), targetSchema) {
					return workflowIssue("binding.invalid", "CEL 表达式无效或返回类型不兼容", nodeID, "", "inputBindings", field)
				}
				expr, err := workflowCELExpr(ast)
				if err != nil {
					return workflowIssue("binding.invalid", "CEL 表达式无效或返回类型不兼容", nodeID, "", "inputBindings", field)
				}
				if celHasArithmetic(expr) && schemaBool(targetSchema, "x-coinsphere-decimal") {
					return workflowIssue("binding.invalid", "金融数值绑定不允许使用 CEL 浮点算术", nodeID, "", "inputBindings", field)
				}
			case "input":
				if binding.NodeInstanceID != "" || len(binding.FieldPath) == 0 || len(binding.Value) != 0 || binding.Expression != "" {
					return workflowIssue("binding.entry", "入口输入绑定格式无效", nodeID, "", "inputBindings", field)
				}
			case "":
				return workflowIssue("binding.invalid", "请选择输入绑定方式", nodeID, "", "inputBindings", field)
			default:
				return workflowIssue("binding.kind", "输入绑定方式无效", nodeID, "", "inputBindings", field)
			}
		}
	}
	return nil
}

func compileWorkflowCEL(expression string) (*cel.Ast, error) {
	return workflowgraph.Compile(expression)
}
func workflowCELExpr(ast *cel.Ast) (*exprpb.Expr, error) {
	checked, err := cel.AstToCheckedExpr(ast)
	if err != nil {
		return nil, err
	}
	return checked.Expr, nil
}

func celHasArithmetic(expr *exprpb.Expr) bool {
	if expr == nil {
		return false
	}
	if call := expr.GetCallExpr(); call != nil {
		switch call.Function {
		case operators.Add, operators.Subtract, operators.Multiply, operators.Divide, operators.Modulo, operators.Negate:
			return true
		}
		if celHasArithmetic(call.Target) {
			return true
		}
		for _, arg := range call.Args {
			if celHasArithmetic(arg) {
				return true
			}
		}
	}
	if selectExpr := expr.GetSelectExpr(); selectExpr != nil {
		return celHasArithmetic(selectExpr.Operand)
	}
	if list := expr.GetListExpr(); list != nil {
		for _, element := range list.Elements {
			if celHasArithmetic(element) {
				return true
			}
		}
	}
	if object := expr.GetStructExpr(); object != nil {
		for _, entry := range object.Entries {
			if celHasArithmetic(entry.GetMapKey()) || celHasArithmetic(entry.Value) {
				return true
			}
		}
	}
	if comprehension := expr.GetComprehensionExpr(); comprehension != nil {
		return celHasArithmetic(comprehension.IterRange) || celHasArithmetic(comprehension.AccuInit) ||
			celHasArithmetic(comprehension.LoopCondition) || celHasArithmetic(comprehension.LoopStep) || celHasArithmetic(comprehension.Result)
	}
	return false
}

func validateWorkflowExpressions(nodes map[string]workflowGraphNode, descriptors map[string]sdk.NodeDescriptor, adjacency map[string][]string, edges []workflowGraphEdge, roots map[string]bool) error {
	validate := func(expression, target string, includeTarget bool) error {
		allowed := map[string]bool{}
		schemas := map[string]json.RawMessage{}
		for id := range nodes {
			if workflowPathExists(adjacency, id, target) || includeTarget && id == target {
				allowed[id] = true
				schemas["nodes."+id] = descriptors[id].OutputSchema
				schemas["incoming."+id+".output"] = descriptors[id].OutputSchema
			}
			if roots[id] && (id == target || workflowPathExists(adjacency, id, target)) {
				schemas["input"] = descriptors[id].InputSchema
			}
		}
		if err := workflowgraph.ValidateNodeReferences(expression, allowed); err != nil {
			return err
		}
		if workflowgraph.HasDecimalArithmetic(expression, schemas) {
			return errors.New("CEL 表达式不能进行 Decimal 算术")
		}
		return nil
	}
	for id, node := range nodes {
		for field, binding := range node.InputBindings {
			if binding.Kind == "cel" {
				if err := validate(binding.Expression, id, false); err != nil {
					return workflowIssue("binding.expression", "输入表达式无效："+err.Error(), id, "", "inputBindings", field)
				}
			}
			if binding.Kind == "input" {
				for root := range roots {
					if root == id || workflowPathExists(adjacency, root, id) {
						source, ok := workflowSchemaField(descriptors[root].InputSchema, binding.FieldPath)
						target, declared := workflowSchemaField(descriptors[id].InputSchema, []string{field})
						if !ok || !declared || !workflowSchemaTypesCompatible(source, target) {
							return workflowIssue("binding.entry", "输入绑定引用了不可用的入口输入字段", id, "", "inputBindings", field)
						}
					}
				}
			}
		}
	}
	for _, edge := range edges {
		if strings.TrimSpace(edge.Condition) != "" {
			if err := validate(edge.Condition, edge.SourceNodeInstanceID, true); err != nil {
				return workflowIssue("edge.condition", "连线条件无效："+err.Error(), "", edge.EdgeID, "condition")
			}
		}
	}
	return nil
}

func ordinaryWorkflowConfigSchema(raw json.RawMessage, secretFields map[string]map[string]any) (json.RawMessage, error) {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	properties, _ := schema["properties"].(map[string]any)
	for field := range secretFields {
		delete(properties, field)
	}
	if required, exists := schema["required"].([]any); exists {
		filtered := make([]any, 0, len(required))
		for _, value := range required {
			field, _ := value.(string)
			if _, secret := secretFields[field]; !secret {
				filtered = append(filtered, value)
			}
		}
		schema["required"] = filtered
	}
	return json.Marshal(schema)
}

func validateWorkflowSchemaValue(raw json.RawMessage, value any) error {
	return workflowgraph.ValidateValue(raw, value)
}

func decodeJSONObject(raw json.RawMessage) (map[string]any, error) {
	var value map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return nil, errors.New("值不是 JSON 对象")
	}
	return value, nil
}

func workflowSchemaProperties(raw json.RawMessage) (map[string]map[string]any, map[string]bool) {
	var schema map[string]any
	_ = json.Unmarshal(raw, &schema)
	properties, _ := schema["properties"].(map[string]any)
	result := make(map[string]map[string]any, len(properties))
	for name, value := range properties {
		if property, ok := value.(map[string]any); ok {
			result[name] = property
		}
	}
	return result, stringSetFromAny(schema["required"])
}

func workflowSchemaField(raw json.RawMessage, path []string) (map[string]any, bool) {
	if len(path) == 0 || len(path) > 32 {
		return nil, false
	}
	var current map[string]any
	if json.Unmarshal(raw, &current) != nil {
		return nil, false
	}
	for _, segment := range path {
		if strings.TrimSpace(segment) == "" || len(segment) > 128 {
			return nil, false
		}
		if types := schemaTypes(current); len(types) > 0 && !types["object"] {
			return nil, false
		}
		properties, _ := current["properties"].(map[string]any)
		next, _ := properties[segment].(map[string]any)
		if next == nil {
			next, _ = current["additionalProperties"].(map[string]any)
			// JSON Schema 未声明 additionalProperties 时允许动态对象字段。
			if current["additionalProperties"] == nil || current["additionalProperties"] == true {
				next = map[string]any{}
			}
		}
		if next == nil {
			return nil, false
		}
		current = next
	}
	return current, true
}

func workflowSchemaTypesCompatible(source, target map[string]any) bool {
	sourceTypes, targetTypes := schemaTypes(source), schemaTypes(target)
	if len(sourceTypes) == 0 || len(targetTypes) == 0 {
		return true
	}
	for sourceType := range sourceTypes {
		if targetTypes[sourceType] || sourceType == "integer" && targetTypes["number"] {
			return true
		}
	}
	return false
}

func workflowCELTypeCompatible(celType string, target map[string]any) bool {
	if celType == "dyn" {
		return true
	}
	types := schemaTypes(target)
	if len(types) == 0 {
		return true
	}
	switch celType {
	case "int", "uint":
		return types["integer"] || types["number"]
	case "double":
		return types["number"]
	case "bool":
		return types["boolean"]
	case "list":
		return types["array"]
	case "map":
		return types["object"]
	default:
		return types[celType]
	}
}

func schemaTypes(schema map[string]any) map[string]bool {
	result := map[string]bool{}
	switch value := schema["type"].(type) {
	case string:
		result[value] = true
	case []any:
		for _, item := range value {
			if text, ok := item.(string); ok {
				result[text] = true
			}
		}
	}
	return result
}

func schemaBool(schema map[string]any, key string) bool {
	value, _ := schema[key].(bool)
	return value
}

func schemaFragment(schema map[string]any) json.RawMessage {
	copy := make(map[string]any, len(schema)+1)
	for key, value := range schema {
		copy[key] = value
	}
	copy["$schema"] = workflowJSONSchema202012
	raw, _ := json.Marshal(copy)
	return raw
}

func workflowPathExists(adjacency map[string][]string, source, target string) bool {
	seen := map[string]bool{}
	queue := append([]string(nil), adjacency[source]...)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == target {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		queue = append(queue, adjacency[current]...)
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
