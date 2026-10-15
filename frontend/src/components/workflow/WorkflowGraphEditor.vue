<template>
  <section class="graph-editor">
    <aside v-if="materialsVisible !== false" class="graph-library" aria-label="节点库">
      <div class="graph-library__header"
        ><strong>节点物料</strong><span>{{ materials.length }}</span></div
      >
      <ElInput v-model="search" placeholder="搜索节点" aria-label="搜索节点" clearable />
      <details v-for="category in categories" :key="category" open class="material-group">
        <summary>{{ category }}</summary>
        <button
          v-for="definition in materials.filter((item) => (item.category || '其他') === category)"
          :key="definition.type"
          class="material"
          :aria-label="definition.title"
          :disabled="readOnly"
          :draggable="!readOnly"
          @dragstart="dragMaterial($event, definition.type)"
          @click="commit(addNode(graph, definition))"
        >
          <span class="material__icon" :style="{ color: definition.color }">{{
            definition.title.slice(0, 1)
          }}</span>
          <span class="material__body"
            ><strong>{{ definition.title }}</strong
            ><small>{{ definition.description }}</small></span
          >
        </button>
      </details>
    </aside>
    <WorkflowGraphCanvas
      ref="canvas"
      hide-controls
      @zoom="emit('zoom', $event)"
      :graph="graph"
      :selected="selectedID"
      :definitions="definitions"
      :readonly="readOnly"
      @update:graph="commit"
      @select="select"
    />
    <aside class="graph-inspector" aria-label="节点与连线编辑">
      <header
        ><strong>{{
          selectedNode
            ? selectedNode.label || definition?.title || selectedNode.nodeType
            : selectedEdge
              ? '连线属性'
              : '工作流属性'
        }}</strong
        ><ElButton text @click="selectedID = ''">关闭</ElButton></header
      >
      <ElScrollbar>
        <template v-if="selectedNode">
          <p>{{ selectedNode.nodeInstanceId }} · {{ selectedNode.nodeVersion }}</p>
          <ElForm :disabled="readOnly" label-position="top"
            ><ElFormItem label="节点名称"
              ><ElInput
                :model-value="selectedNode.label || ''"
                :placeholder="definition?.title"
                :maxlength="80"
                @update:model-value="
                  updateNode((node) => {
                    node.label = $event
                  })
                " /></ElFormItem
          ></ElForm>
          <ElTabs v-model="tab">
            <ElTabPane label="配置" name="config">
              <ElAlert
                v-if="componentError"
                :title="componentError"
                type="error"
                :closable="false"
              />
              <ElForm :disabled="readOnly" label-position="top"
                ><WorkflowConditionEditor
                  v-if="selectedNode.nodeType === 'core.condition'"
                  :config="selectedNode.config"
                  :fields="inputFields"
                  @update="setConfig" /><component
                  v-else
                  :is="nodeEditor || WorkflowSchemaFields"
                  :schema="configSchema"
                  :ui-schema="definition?.uiSchema"
                  :config="selectedNode.config"
                  @update="setConfig"
              /></ElForm>
              <ElButton v-if="selectedNode.nodeType === 'core.loop'" @click="loopVisible = true"
                >编辑循环子图</ElButton
              >
            </ElTabPane>
            <ElTabPane label="输入绑定" name="bindings">
              <ElForm :disabled="readOnly" label-position="top">
                <ElFormItem
                  v-for="field in inputFields"
                  :key="field"
                  :label="
                    String((definition?.inputSchema as any)?.properties?.[field]?.title || field)
                  "
                >
                  <div class="binding-row">
                    <ElSelect
                      :model-value="selectedNode.inputBindings?.[field]?.kind || ''"
                      placeholder="未绑定"
                      @update:model-value="setBindingKind(field, $event)"
                      ><ElOption label="未绑定" value="" /><ElOption
                        label="固定值"
                        value="literal" /><ElOption label="节点输出" value="field" /><ElOption
                        label="入口输入"
                        value="input" /><ElOption label="表达式" value="cel"
                    /></ElSelect>
                    <template
                      v-if="
                        ['field', 'input'].includes(selectedNode.inputBindings?.[field]?.kind || '')
                      "
                    >
                      <ElSelect
                        v-if="selectedNode.inputBindings?.[field]?.kind === 'field'"
                        :model-value="selectedNode.inputBindings?.[field]?.nodeInstanceId"
                        placeholder="来源节点"
                        @update:model-value="patchBinding(field, { nodeInstanceId: $event })"
                        ><ElOption
                          v-for="node in graph.nodes.filter((node) =>
                            upstream.has(node.nodeInstanceId)
                          )"
                          :key="node.nodeInstanceId"
                          :label="nodeName(node.nodeInstanceId)"
                          :value="node.nodeInstanceId"
                      /></ElSelect>
                      <ElSelect
                        filterable
                        allow-create
                        :model-value="selectedNode.inputBindings?.[field]?.fieldPath?.join('.')"
                        placeholder="输出字段路径"
                        @change="
                          patchBinding(field, {
                            fieldPath: String($event).split('.').filter(Boolean)
                          })
                        "
                        ><ElOption
                          v-for="path in bindingPaths(field)"
                          :key="path"
                          :value="path"
                          :label="path"
                      /></ElSelect>
                    </template>
                    <ElInput
                      v-if="selectedNode.inputBindings?.[field]?.kind === 'cel'"
                      :model-value="selectedNode.inputBindings?.[field]?.expression"
                      type="textarea"
                      placeholder="nodes['节点ID'].字段 / input.字段"
                      @update:model-value="patchBinding(field, { expression: $event })"
                    />
                    <ElInput
                      v-if="selectedNode.inputBindings?.[field]?.kind === 'literal'"
                      :model-value="literalText(field)"
                      :placeholder="literalIsString(field) ? '固定文本' : 'JSON 值'"
                      @update:model-value="literalDrafts[field] = $event"
                      @change="setLiteral(field, $event)"
                    />
                  </div>
                </ElFormItem>
              </ElForm>
              <ElInput
                :disabled="readOnly"
                v-model="bindingName"
                placeholder="其他输入字段"
              /><ElButton :disabled="readOnly" @click="addBinding">添加绑定</ElButton>
            </ElTabPane>
            <ElTabPane label="凭据" name="secrets">
              <ElEmpty v-if="!definition?.secretFields.length" description="此节点不需要凭据" />
              <ElForm :disabled="readOnly" label-position="top"
                ><ElFormItem
                  v-for="field in definition?.secretFields"
                  :key="field.name"
                  :label="field.title || field.name"
                >
                  <ElTag>{{
                    secretFields?.[qualifiedID]?.[field.name] ? '已配置' : '未配置'
                  }}</ElTag>
                  <ElInput
                    :model-value="
                      secretChanges.find(
                        (change) =>
                          change.nodeInstanceId === qualifiedID && change.field === field.name
                      )?.value || ''
                    "
                    type="password"
                    autocomplete="new-password"
                    placeholder="输入新值以替换；留空保持原值"
                    :disabled="readOnly || !canManageSecrets"
                    @update:model-value="setSecret(field.name, $event)"
                  />
                  <ElButton
                    :disabled="readOnly || !canManageSecrets"
                    link
                    type="danger"
                    @click="setSecret(field.name, '', true)"
                    >移除</ElButton
                  >
                </ElFormItem></ElForm
              >
            </ElTabPane>
          </ElTabs>
          <ElButton type="danger" plain :disabled="readOnly" @click="removeSelectedNode"
            >删除节点</ElButton
          >
        </template>
        <ElForm v-else-if="selectedEdge" :disabled="readOnly" label-position="top"
          ><ElFormItem label="连线名称"
            ><ElInput
              :model-value="selectedEdge.label || ''"
              :maxlength="80"
              placeholder="画布上显示的简短名称"
              @update:model-value="patchEdge({ label: $event })" /></ElFormItem
          ><ElFormItem label="来源端口"
            ><ElSelect
              :model-value="selectedEdge.sourcePort"
              @update:model-value="patchEdge({ sourcePort: $event })"
              ><ElOption
                v-for="port in edgePorts('source')"
                :key="port"
                :label="portLabel(port) || '输出'"
                :value="port" /></ElSelect></ElFormItem
          ><ElFormItem label="目标端口"
            ><ElSelect
              :model-value="selectedEdge.targetPort"
              @update:model-value="patchEdge({ targetPort: $event })"
              ><ElOption
                v-for="port in edgePorts('target')"
                :key="port"
                :label="portLabel(port) || '输入'"
                :value="port" /></ElSelect></ElFormItem
          ><ElFormItem label="执行条件"
            ><ElInput
              :model-value="selectedEdge.condition || ''"
              type="textarea"
              placeholder="留空表示无条件执行"
              @update:model-value="patchEdge({ condition: $event })" /></ElFormItem
          ><ElButton type="danger" @click="deleteEdge">删除连线</ElButton></ElForm
        >
        <div v-else class="workflow-properties"
          ><p>选择节点或连线编辑属性；拖动物料到画布添加节点。</p>
          <details class="entry-settings"
            ><summary>入口与连线</summary>
            <ElDivider>入口</ElDivider>
            <div v-for="(id, name) in graph.entryPoints" :key="name" class="entry-row">
              <span>{{ name }}</span
              ><ElSelect
                :disabled="readOnly"
                :model-value="id"
                @update:model-value="setEntry(String(name), $event)"
                ><ElOption
                  v-for="node in graph.nodes"
                  :key="node.nodeInstanceId"
                  :value="node.nodeInstanceId"
                  :label="node.nodeInstanceId"
              /></ElSelect>
              <ElButton link type="danger" :disabled="readOnly" @click="removeEntry(String(name))"
                >删除</ElButton
              >
            </div>
            <ElInput
              :disabled="readOnly"
              v-model="entryName"
              placeholder="新入口名称"
              aria-label="新入口名称"
            /><ElButton :disabled="readOnly" @click="addEntry">添加入口</ElButton>
          </details></div
        >
      </ElScrollbar>
    </aside>
    <ElDialog v-model="loopVisible" title="循环子图" width="95%" destroy-on-close>
      <WorkflowGraphEditor
        v-if="selectedNode && loopVisible"
        :graph="selectedNode.config.body as WorkflowGraph"
        :definitions="definitions"
        :secret-fields="secretFields"
        :secret-changes="secretChanges"
        :can-manage-secrets="canManageSecrets"
        :prefix="`${qualifiedID}.`"
        :read-only="readOnly"
        body
        @update:graph="setConfig('body', $event)"
        @update:secret-changes="emit('update:secretChanges', $event)"
      />
    </ElDialog>
  </section>
</template>
<script setup lang="ts">
  import { ElMessage } from 'element-plus'
  import type { Component } from 'vue'
  import type {
    WorkflowGraph,
    WorkflowGraphEdge,
    WorkflowGraphNode,
    WorkflowInputBinding,
    WorkflowNodeDefinition,
    WorkflowSecretChange
  } from '@/api/workflows'
  import { loadPluginNodeEditor } from '@/plugins'
  import WorkflowGraphCanvas from './WorkflowGraphCanvas.vue'
  import WorkflowSchemaFields from './WorkflowSchemaFields.vue'
  import WorkflowConditionEditor from './WorkflowConditionEditor.vue'
  import { addNode, cloneGraph, removeNode, ancestors, portLabel } from './graph'
  const props = withDefaults(
    defineProps<{
      graph: WorkflowGraph
      definitions: WorkflowNodeDefinition[]
      secretFields?: Record<string, Record<string, boolean>>
      secretChanges: WorkflowSecretChange[]
      canManageSecrets: boolean
      readOnly?: boolean
      materialsVisible?: boolean
      prefix?: string
      body?: boolean
    }>(),
    { prefix: '' }
  )
  const emit = defineEmits<{
    (event: 'update:graph', graph: WorkflowGraph): void
    (event: 'zoom', scale: number): void
    (event: 'update:secretChanges', changes: WorkflowSecretChange[]): void
  }>()
  const canvas = ref<InstanceType<typeof WorkflowGraphCanvas>>()
  defineExpose({
    zoom: (amount: number) => canvas.value?.zoom(amount),
    fit: () => canvas.value?.fit(),
    center: () => canvas.value?.center()
  })
  const dragMaterial = (event: DragEvent, type: string) => {
    if (props.readOnly) return
    event.dataTransfer?.setData('application/coinsphere-node', type)
  }
  const search = ref(''),
    entryName = ref(''),
    bindingName = ref(''),
    selectedID = ref(''),
    selectedKind = ref('node'),
    tab = ref('config')
  const loopVisible = ref(false),
    componentError = ref('')
  const nodeEditor = shallowRef<Component>()
  const materials = computed(() =>
    props.definitions.filter(
      (item) =>
        (props.body
          ? item.available &&
            item.kind !== 'trigger' &&
            !['core.loop', 'core.end', 'core.human_approval'].includes(item.type)
          : item.available) &&
        `${item.title} ${item.type}`.toLowerCase().includes(search.value.toLowerCase())
    )
  )
  const categories = computed(() => [
    ...new Set(materials.value.map((item) => item.category || '其他'))
  ])
  const selectedNode = computed(() =>
    selectedKind.value === 'node'
      ? props.graph.nodes.find((node) => node.nodeInstanceId === selectedID.value)
      : undefined
  )
  const selectedEdge = computed(() =>
    selectedKind.value === 'edge'
      ? props.graph.edges.find((edge) => edge.edgeId === selectedID.value)
      : undefined
  )
  const definition = computed(() =>
    props.definitions.find((item) => item.type === selectedNode.value?.nodeType)
  )
  const qualifiedID = computed(() => props.prefix + (selectedNode.value?.nodeInstanceId || ''))
  const configSchema = computed(() => {
    const schema = JSON.parse(JSON.stringify(definition.value?.configSchema || {}))
    const properties = schema.properties as Record<string, unknown> | undefined
    if (properties) {
      delete properties.body
      for (const field of definition.value?.secretFields || []) delete properties[field.name]
    }
    return schema
  })
  const inputFields = computed(() => [
    ...new Set([
      ...Object.keys((definition.value?.inputSchema as any)?.properties || {}),
      ...Object.keys(selectedNode.value?.inputBindings || {}),
      ...(selectedNode.value?.nodeType === 'core.condition'
        ? ((selectedNode.value.config.rules as Array<{ fieldPath: string[] }>) || [])
            .map((rule) => rule.fieldPath[0])
            .filter(Boolean)
        : [])
    ])
  ])
  const upstream = computed(() => ancestors(props.graph, selectedID.value))
  const nodeName = (id: string) => {
    const node = props.graph.nodes.find((node) => node.nodeInstanceId === id)
    return (
      node?.label || props.definitions.find((item) => item.type === node?.nodeType)?.title || id
    )
  }
  const edgePorts = (end: 'source' | 'target') => {
    const id =
      end === 'source'
        ? selectedEdge.value?.sourceNodeInstanceId
        : selectedEdge.value?.targetNodeInstanceId
    const node = props.graph.nodes.find((node) => node.nodeInstanceId === id)
    const definition = props.definitions.find((item) => item.type === node?.nodeType)
    return (end === 'source' ? definition?.outputPorts : definition?.inputPorts) || []
  }
  const literalIsString = (field: string) =>
    (definition.value?.inputSchema as any)?.properties?.[field]?.type === 'string'
  const literalDrafts = ref<Record<string, string>>({})
  watch(selectedID, () => {
    literalDrafts.value = {}
  })
  const literalText = (field: string) =>
    literalDrafts.value[field] ??
    (literalIsString(field)
      ? String(selectedNode.value?.inputBindings?.[field]?.value ?? '')
      : JSON.stringify(selectedNode.value?.inputBindings?.[field]?.value))
  const bindingPaths = (field: string) => {
    const binding = selectedNode.value?.inputBindings?.[field]
    const node = props.graph.nodes.find((node) => node.nodeInstanceId === binding?.nodeInstanceId)
    const schema =
      props.definitions.find((item) => item.type === node?.nodeType)?.outputSchema || {}
    const paths: string[] = []
    const visit = (schema: any, path: string[]) => {
      if (path.length) paths.push(path.join('.'))
      if (path.length < 12)
        for (const [name, property] of Object.entries(schema.properties || {}))
          visit(property, [...path, name])
    }
    visit(schema, [])
    return paths
  }
  const commit = (graph: WorkflowGraph) => {
    if (!props.readOnly) emit('update:graph', graph)
  }
  const removeSelectedNode = () => {
    if (!selectedNode.value || props.readOnly) return
    commit(removeNode(props.graph, selectedNode.value.nodeInstanceId))
    selectedID.value = ''
  }
  const updateNode = (update: (node: WorkflowGraphNode) => void) => {
    const graph = cloneGraph(props.graph)
    const node = graph.nodes.find((node) => node.nodeInstanceId === selectedID.value)
    if (node) {
      update(node)
      commit(graph)
    }
  }
  const select = (id: string, kind: 'node' | 'edge') => {
    selectedID.value = id
    selectedKind.value = kind
  }
  let componentRequest = 0
  watch(
    () => definition.value,
    async (current) => {
      const request = ++componentRequest
      nodeEditor.value = undefined
      componentError.value = ''
      if (!current?.pluginId || !current.editorKey) return
      try {
        const component = await loadPluginNodeEditor(current.editorKey, current.pluginId)
        if (request === componentRequest) nodeEditor.value = component
      } catch (error: any) {
        if (request === componentRequest) componentError.value = error.message
      }
    }
  )
  const setConfig = (key: string, value: unknown) =>
    updateNode((node) => {
      node.config[key] = value
    })
  const patchBinding = (field: string, patch: Partial<WorkflowInputBinding>) =>
    updateNode((node) => {
      node.inputBindings ??= {}
      node.inputBindings[field] = { ...node.inputBindings[field], ...patch }
    })
  const setBindingKind = (field: string, kind: WorkflowInputBinding['kind'] | '') =>
    updateNode((node) => {
      node.inputBindings ??= {}
      if (!kind) delete node.inputBindings[field]
      else
        node.inputBindings[field] =
          kind === 'literal'
            ? { kind, value: '' }
            : kind === 'field' || kind === 'input'
              ? { kind, ...(kind === 'field' ? { nodeInstanceId: '' } : {}), fieldPath: [] }
              : { kind, expression: '' }
    })
  const setLiteral = (field: string, value: string) => {
    try {
      patchBinding(field, { value: literalIsString(field) ? value : JSON.parse(value) })
      delete literalDrafts.value[field]
    } catch {
      ElMessage.error('请输入有效的 JSON 值')
    }
  }
  const addBinding = () => {
    if (bindingName.value.trim()) setBindingKind(bindingName.value.trim(), 'literal')
    bindingName.value = ''
  }
  const setSecret = (field: string, value: string, remove = false) => {
    if (!props.canManageSecrets || props.readOnly) return
    const changes = props.secretChanges.filter(
      (change) => change.nodeInstanceId !== qualifiedID.value || change.field !== field
    )
    if (value || remove)
      changes.push({
        nodeInstanceId: qualifiedID.value,
        field,
        ...(remove ? { remove: true } : { value })
      })
    emit('update:secretChanges', changes)
  }
  const patchEdge = (patch: Partial<WorkflowGraphEdge>) => {
    const graph = cloneGraph(props.graph)
    const edge = graph.edges.find((edge) => edge.edgeId === selectedID.value)
    if (edge) {
      Object.assign(edge, patch)
      commit(graph)
    }
  }
  const deleteEdge = () => {
    const graph = cloneGraph(props.graph)
    graph.edges = graph.edges.filter((edge) => edge.edgeId !== selectedID.value)
    commit(graph)
    selectedID.value = ''
  }
  const setEntry = (name: string, id: string) => {
    const graph = cloneGraph(props.graph)
    graph.entryPoints[name] = id
    commit(graph)
  }
  const removeEntry = (name: string) => {
    const graph = cloneGraph(props.graph)
    delete graph.entryPoints[name]
    commit(graph)
  }
  const addEntry = () => {
    const name = entryName.value.trim()
    if (!/^[a-zA-Z][a-zA-Z0-9_-]*$/.test(name) || !props.graph.nodes.length) {
      ElMessage.warning('请输入有效入口名称并先添加节点')
      return
    }
    setEntry(name, props.graph.nodes[0].nodeInstanceId)
    entryName.value = ''
  }
</script>
<style scoped lang="scss">
  .graph-editor {
    display: flex;
    position: relative;
    width: 100%;
    height: 100%;
    min-height: 460px;
    border: 1px solid var(--workflow-panel-border);
    border-radius: 8px;
    overflow: hidden;
  }
  .graph-library {
    flex: 0 0 240px;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    overflow: auto;
    background: var(--workflow-panel-bg);
    border-right: 1px solid var(--workflow-panel-border);
  }
  .graph-editor > :deep(.graph-surface) {
    flex: 1 1 0;
    min-width: 260px;
    border-radius: 0;
  }
  .graph-library__header,
  .graph-inspector header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 13px;
    color: var(--workflow-panel-text);
    border-bottom: 1px solid var(--workflow-panel-border);
    padding-bottom: 8px;
  }
  .graph-library__header span,
  summary {
    font-size: 11px;
    color: var(--workflow-panel-muted);
  }
  summary {
    cursor: pointer;
    padding: 6px 0;
    font-weight: 700;
  }
  .material {
    box-sizing: border-box;
    display: flex;
    gap: 8px;
    align-items: center;
    width: 100%;
    margin: 4px 0;
    padding: 5px 8px;
    text-align: left;
    cursor: grab;
    background: var(--workflow-panel-raised);
    color: var(--workflow-panel-text);
    border: 1px solid var(--workflow-panel-border);
    border-radius: 6px;
  }
  .material__icon {
    display: grid;
    place-items: center;
    flex: 0 0 26px;
    height: 26px;
    background: var(--workflow-panel-soft);
    border-radius: 6px;
  }
  .material__body {
    min-width: 0;
  }
  .material strong {
    display: block;
    font-size: 13px;
    line-height: 17px;
  }
  .material small {
    display: block;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-size: 11px;
    line-height: 15px;
    color: var(--workflow-panel-muted);
  }
  .material:hover,
  .material:focus-visible {
    border-color: var(--theme-color);
  }
  .graph-inspector {
    flex: 0 0 340px;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    padding: 14px;
    background: var(--workflow-panel-bg);
    color: var(--workflow-panel-text);
    border-left: 1px solid var(--workflow-panel-border);
  }
  .graph-inspector :deep(.el-scrollbar) {
    flex: 1;
    min-height: 0;
  }
  .workflow-properties {
    font-size: 13px;
    color: var(--workflow-panel-muted);
  }
  .entry-row,
  .binding-row {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
  }
  @media (max-width: 1100px) {
    .graph-library {
      flex-basis: 200px;
    }
    .graph-inspector {
      flex-basis: 290px;
    }
  }
  @media (max-width: 768px) {
    .graph-editor {
      overflow-x: auto;
    }
    .graph-library {
      flex-basis: 180px;
    }
    .graph-inspector {
      flex-basis: 280px;
    }
  }
</style>
