import type { WorkflowGraph, WorkflowGraphNode, WorkflowNodeDefinition } from '@/api/workflows'
import { runStatusLabel } from './status'

export const cloneGraph = (graph: WorkflowGraph): WorkflowGraph => JSON.parse(JSON.stringify(graph))
export const emptyGraph = (): WorkflowGraph => ({
  schemaVersion: 3,
  entryPoints: {},
  nodes: [],
  edges: []
})

export function configDefaults(schema: Record<string, any>): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(schema.properties ?? {}).flatMap(([key, field]: [string, any]) =>
      field.default === undefined ? [] : [[key, JSON.parse(JSON.stringify(field.default))]]
    )
  )
}

export function addNode(graph: WorkflowGraph, definition: WorkflowNodeDefinition): WorkflowGraph {
  const next = cloneGraph(graph)
  const id = `node-${crypto.randomUUID()}`
  next.nodes.push({
    nodeInstanceId: id,
    nodeType: definition.type,
    nodeVersion: definition.version,
    config: configDefaults(definition.configSchema),
    inputBindings: {},
    position: { x: 100 + next.nodes.length * 40, y: 100 }
  })
  if (definition.kind === 'trigger' && !next.entryPoints.main) next.entryPoints.main = id
  return next
}

export function removeNode(graph: WorkflowGraph, id: string): WorkflowGraph {
  const next = cloneGraph(graph)
  next.nodes = next.nodes.filter((node) => node.nodeInstanceId !== id)
  next.edges = next.edges.filter(
    (edge) => edge.sourceNodeInstanceId !== id && edge.targetNodeInstanceId !== id
  )
  for (const [name, nodeID] of Object.entries(next.entryPoints))
    if (nodeID === id) delete next.entryPoints[name]
  return next
}

export function canvasNode(
  node: WorkflowGraphNode,
  definition?: WorkflowNodeDefinition,
  states: Record<string, string> = {},
  selectedID = ''
) {
  const state = states[node.nodeInstanceId]
  const color =
    (
      {
        failed: '#dc2626',
        succeeded: '#16a34a',
        running: '#2563eb',
        waiting: '#d97706',
        retrying: '#d97706'
      } as Record<string, string>
    )[state] ||
    definition?.color ||
    '#64748b'
  return {
    id: node.nodeInstanceId,
    shape: 'rect',
    ...node.position,
    width: 210,
    height: 70,
    label: `${node.label || definition?.title || node.nodeType}${state ? '\n' + runStatusLabel(state) : ''}`,
    data: { executionState: state },
    attrs: {
      body: {
        fill: 'var(--el-bg-color)',
        stroke: color,
        strokeWidth: selectedID === node.nodeInstanceId ? 3 : 2,
        rx: 8,
        ry: 8
      },
      label: {
        fill: 'var(--el-text-color-primary)',
        fontSize: 13,
        textWrap: { width: 186, height: 55, ellipsis: true }
      }
    },
    ports: {
      groups: {
        in: {
          position: 'left',
          attrs: { circle: { r: 5, magnet: 'passive', stroke: color, fill: 'var(--el-bg-color)' } }
        },
        out: {
          position: 'right',
          attrs: {
            circle: { r: 5, magnet: true, stroke: color, fill: 'var(--el-bg-color)' },
            text: { fill: 'var(--el-text-color-secondary)', fontSize: 10 }
          },
          label: { position: 'right' }
        }
      },
      items: [
        ...(definition?.inputPorts ?? ['in']).map((id) => ({ id: `in:${id}`, group: 'in' })),
        ...(definition?.outputPorts ?? ['out']).map((id) => ({
          id: `out:${id}`,
          group: 'out',
          attrs: { text: { text: portLabel(id) } }
        }))
      ]
    }
  }
}

export const portLabel = (port: string) =>
  (
    ({
      out: '',
      true: '满足',
      false: '不满足',
      unavailable: '数据不可用',
      matched: '跟涨 / 跟跌',
      timeout: '到期未达到',
      completed: '完成'
    }) as Record<string, string>
  )[port] ?? port

export function canvasEdges(graph: WorkflowGraph, selectedID = '') {
  return graph.edges.map((edge) => ({
    id: edge.edgeId,
    shape: 'edge',
    source: { cell: edge.sourceNodeInstanceId, port: `out:${edge.sourcePort}` },
    target: { cell: edge.targetNodeInstanceId, port: `in:${edge.targetPort}` },
    router: { name: 'manhattan' },
    connector: { name: 'rounded' },
    attrs: {
      line: {
        stroke: selectedID === edge.edgeId ? '#2563eb' : '#94a3b8',
        strokeWidth: 1.5,
        targetMarker: 'block'
      }
    },
    labels:
      edge.label || portLabel(edge.sourcePort) ? [edge.label || portLabel(edge.sourcePort)] : []
  }))
}

export function canvasCells(
  graph: WorkflowGraph,
  definitions: WorkflowNodeDefinition[],
  states: Record<string, string> = {},
  selectedID = ''
) {
  return [
    ...graph.nodes.map((node) =>
      canvasNode(
        node,
        definitions.find((item) => item.type === node.nodeType),
        states,
        selectedID
      )
    ),
    ...canvasEdges(graph, selectedID)
  ]
}
export function ancestors(graph: WorkflowGraph, id: string): Set<string> {
  const found = new Set<string>()
  const pending = [id]
  while (pending.length) {
    const current = pending.pop()
    for (const edge of graph.edges.filter((edge) => edge.targetNodeInstanceId === current)) {
      if (!found.has(edge.sourceNodeInstanceId) && edge.sourceNodeInstanceId !== id) {
        found.add(edge.sourceNodeInstanceId)
        pending.push(edge.sourceNodeInstanceId)
      }
    }
  }
  return found
}

export function schemaFieldPaths(
  schema: Record<string, any>,
  path: string[] = [],
  titles: string[] = []
): { value: string; label: string }[] {
  if (path.length >= 12) return []
  return Object.entries(schema.properties || {}).flatMap(([name, property]: [string, any]) => {
    const next = [...path, name],
      labels = [...titles, String(property.title || name)]
    return [
      { value: next.join('.'), label: labels.join(' / ') },
      ...schemaFieldPaths(property, next, labels)
    ]
  })
}
