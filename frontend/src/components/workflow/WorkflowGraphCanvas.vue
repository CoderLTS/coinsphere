<template>
  <div class="graph-surface">
    <div v-if="!hideControls" class="graph-actions"
      ><ElButton @click="canvas?.zoom(-0.1)">缩小</ElButton
      ><ElButton @click="canvas?.zoom(0.1)">放大</ElButton
      ><ElButton @click="canvas?.zoomToFit({ maxScale: 1 })">适应画布</ElButton></div
    >
    <div
      v-if="context"
      class="graph-context"
      :style="{ left: `${context.x}px`, top: `${context.y}px` }"
      @contextmenu.prevent
    >
      <button @click="editContext">编辑属性</button>
      <button v-if="!readonly" class="danger" @click="removeContext"
        >删除{{ context.kind === 'node' ? '节点' : '连线' }}</button
      >
    </div>
    <div
      ref="container"
      class="graph-canvas"
      @dragover.prevent
      @drop="drop"
      role="region"
      aria-label="工作流画布"
    />
  </div>
</template>
<script setup lang="ts">
  import { Graph } from '@antv/x6'
  import type { WorkflowGraph, WorkflowNodeDefinition } from '@/api/workflows'
  import { canvasCells, cloneGraph, addNode, removeNode } from './graph'
  const props = defineProps<{
    graph: WorkflowGraph
    definitions: WorkflowNodeDefinition[]
    addableTypes?: string[]
    readonly?: boolean
    hideControls?: boolean
    states?: Record<string, string>
    selected?: string
  }>()
  const emit = defineEmits<{
    (event: 'update:graph', graph: WorkflowGraph): void
    (event: 'select', id: string, kind: 'node' | 'edge'): void
    (event: 'zoom', scale: number): void
  }>()
  const container = ref<HTMLElement>()
  const context = ref<{ id: string; kind: 'node' | 'edge'; x: number; y: number }>()
  const editContext = () => {
    if (!context.value) return
    emit('select', context.value.id, context.value.kind)
    context.value = undefined
  }
  const removeContext = () => {
    if (!context.value || props.readonly) return
    const next =
      context.value.kind === 'node'
        ? removeNode(props.graph, context.value.id)
        : cloneGraph(props.graph)
    if (context.value.kind === 'edge')
      next.edges = next.edges.filter((edge) => edge.edgeId !== context.value!.id)
    context.value = undefined
    emit('update:graph', next)
  }
  let canvas: Graph | undefined
  let rendering = false
  let selectedID = ''
  const rendered = new Map<string, string>()
  const render = () => {
    if (!canvas) return
    rendering = true
    try {
      const models = canvasCells(props.graph, props.definitions, props.states, selectedID)
      const ids = new Set(models.map((model) => model.id))
      for (const cell of canvas.getCells())
        if (!ids.has(cell.id)) {
          canvas.removeCell(cell)
          rendered.delete(cell.id)
        }
      for (const model of models) {
        const encoded = JSON.stringify(model)
        if (rendered.get(model.id) === encoded && canvas.getCellById(model.id)) continue
        const cell = canvas.getCellById(model.id)
        if (!cell) {
          if ('ports' in model) canvas.addNode(model)
          else canvas.addEdge(model)
        } else if (cell.isNode() && 'ports' in model) {
          cell.position(model.x, model.y)
          cell.size(model.width, model.height)
          cell.attr(model.attrs)
          cell.attr('label/text', model.label)
          cell.setProp('ports', model.ports)
          cell.setData(model.data)
        } else if (cell.isEdge() && 'source' in model) {
          cell.setSource(model.source)
          cell.setTarget(model.target)
          cell.attr(model.attrs)
          cell.setLabels(model.labels)
        }
        rendered.set(model.id, encoded)
      }
    } finally {
      rendering = false
    }
  }
  watch(
    () => props.selected,
    (id) => {
      selectedID = id || ''
      render()
    }
  )
  const zoom = (amount: number) => canvas?.zoom(amount)
  const fit = () => canvas?.zoomToFit({ padding: 90, maxScale: 1 })
  const center = () => canvas?.centerContent()
  const drop = (event: DragEvent) => {
    if (props.readonly || !canvas) return
    const type = event.dataTransfer?.getData('application/coinsphere-node')
    const definition = props.definitions.find(
      (item) =>
        item.type === type &&
        (props.addableTypes ? props.addableTypes.includes(item.type) : item.available)
    )
    if (!definition) return
    const next = addNode(props.graph, definition)
    const point = canvas.clientToLocal(event.clientX, event.clientY)
    next.nodes[next.nodes.length - 1].position = { x: point.x, y: point.y }
    emit('update:graph', next)
  }
  defineExpose({
    zoom,
    fit,
    center,
    focus: (id: string) => {
      const cell = canvas?.getCellById(id)
      if (cell) canvas?.centerCell(cell)
    }
  })
  onMounted(() => {
    if (!container.value) return
    canvas = new Graph({
      container: container.value,
      autoResize: true,
      async: false,
      grid: true,
      panning: true,
      mousewheel: { enabled: true, minScale: 0.2, maxScale: 2, modifiers: ['ctrl', 'meta'] },
      interacting: () => !props.readonly,
      connecting: {
        allowBlank: false,
        allowLoop: false,
        allowNode: false,
        allowEdge: false,
        snap: true,
        router: { name: 'manhattan' },
        connector: { name: 'rounded' },
        createEdge: () =>
          canvas!.createEdge({
            shape: 'edge',
            attrs: { line: { stroke: '#94a3b8', strokeWidth: 1.5, targetMarker: 'block' } }
          }),
        validateConnection: ({ sourcePort, targetPort }) =>
          Boolean(sourcePort?.startsWith('out:') && targetPort?.startsWith('in:'))
      }
    })
    canvas.on('scale', ({ sx }) => emit('zoom', sx))
    canvas.on('node:click', ({ node }) => {
      selectedID = node.id
      render()
      emit('select', node.id, 'node')
    })
    canvas.on('edge:click', ({ edge }) => {
      selectedID = edge.id
      render()
      emit('select', edge.id, 'edge')
    })
    canvas.on('node:mouseup', ({ node }) => {
      if (props.readonly || rendering) return
      const next = cloneGraph(props.graph)
      const item = next.nodes.find((item) => item.nodeInstanceId === node.id)
      if (
        item &&
        (item.position.x !== node.position().x || item.position.y !== node.position().y)
      ) {
        item.position = node.position()
        emit('update:graph', next)
      }
    })
    canvas.on('edge:connected', ({ edge }) => {
      if (props.readonly || rendering) return
      const source = edge.getSourceCellId(),
        target = edge.getTargetCellId()
      if (!source || !target) return
      const next = cloneGraph(props.graph)
      const item = {
        edgeId: edge.id,
        sourceNodeInstanceId: source,
        sourcePort: (edge.getSourcePortId() ?? '').replace(/^out:/, ''),
        targetNodeInstanceId: target,
        targetPort: (edge.getTargetPortId() ?? '').replace(/^in:/, '')
      }
      const old = next.edges.findIndex((item) => item.edgeId === edge.id)
      if (old < 0) next.edges.push(item)
      else next.edges[old] = { ...next.edges[old], ...item }
      emit('update:graph', next)
    })
    canvas.on('blank:click', () => {
      context.value = undefined
    })
    canvas.on('cell:contextmenu', ({ cell, e }) => {
      e.preventDefault()
      const bounds = container.value!.getBoundingClientRect()
      context.value = {
        id: cell.id,
        kind: cell.isNode() ? 'node' : 'edge',
        x: Math.max(0, Math.min(e.clientX - bounds.left, bounds.width - 170)),
        y: Math.max(0, Math.min(e.clientY - bounds.top, bounds.height - 100))
      }
    })
    render()
    fit()
  })
  watch(() => [props.graph, props.definitions, props.states], render, { deep: true })
  onBeforeUnmount(() => canvas?.dispose())
</script>
<style scoped>
  .graph-surface {
    position: relative;
    min-width: 0;
    height: 100%;
    min-height: 420px;
    background: var(--workflow-canvas-bg, var(--el-fill-color-lighter));
    border-radius: 8px;
  }
  .graph-canvas {
    width: 100%;
    height: 100%;
    min-height: 420px;
  }
  .graph-actions {
    position: absolute;
    z-index: 1;
    right: 16px;
    bottom: 16px;
  }
  .graph-context {
    position: absolute;
    z-index: 35;
    width: 160px;
    padding: 6px;
    background: var(--workflow-panel-bg);
    border: 1px solid var(--workflow-panel-border);
    border-radius: 8px;
    box-shadow: 0 10px 26px rgb(15 23 42 / 0.14);
  }
  .graph-context button {
    display: block;
    width: 100%;
    padding: 8px 12px;
    text-align: left;
    font: inherit;
    cursor: pointer;
    color: var(--workflow-panel-text);
    border: 0;
    background: transparent;
    border-radius: 6px;
  }
  .graph-context button:hover {
    background: var(--workflow-panel-soft);
    color: var(--theme-color);
  }
  .graph-context .danger:hover {
    color: var(--el-color-danger);
  }
</style>
