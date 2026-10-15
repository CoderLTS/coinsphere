import assert from 'node:assert/strict'
import { test } from 'node:test'
import { addNode, canvasCells, cloneGraph, removeNode, schemaFieldPaths } from './graph'
import { readRouteFilters } from '../../hooks/core/useRouteFilters'
import { schemaOutputFields } from './output-summary'
import type { WorkflowGraph, WorkflowNodeDefinition } from '../../api/workflows'

const fixture: WorkflowGraph = {
  schemaVersion: 3,
  entryPoints: { main: 'events', manualReview: 'review' },
  nodes: [
    {
      nodeInstanceId: 'events',
      nodeType: 'core.event',
      nodeVersion: '1.0.0',
      config: {
        types: ['business.created', 'business.updated'],
        source: 'urn:business',
        subject: 'case',
        partitionKey: 'event.subject'
      },
      position: { x: 33, y: 74 }
    },
    {
      nodeInstanceId: 'review',
      label: '业务审核',
      nodeType: 'test.business.review',
      nodeVersion: '2.0.0',
      config: { enabled: false },
      inputBindings: {
        source: { kind: 'field', nodeInstanceId: 'events', fieldPath: ['items', '0', 'name'] },
        job: { kind: 'input', fieldPath: ['job'] },
        fixed: { kind: 'literal', value: { labels: ['a', 'b'] } },
        expression: { kind: 'cel', expression: 'nodes.events.items[input.index].name' }
      },
      position: { x: 333, y: 174 }
    },
    {
      nodeInstanceId: 'loop',
      nodeType: 'core.loop',
      nodeVersion: '1.0.0',
      config: {
        maxIterations: 4,
        timeoutSeconds: 30,
        exitCondition: 'input.iteration == 3',
        body: {
          schemaVersion: 3,
          entryPoints: {},
          nodes: [
            {
              nodeInstanceId: 'item',
              nodeType: 'core.loop_item',
              nodeVersion: '1.0.0',
              config: {},
              position: { x: 1, y: 2 }
            }
          ],
          edges: []
        }
      },
      position: { x: 533, y: 174 }
    }
  ],
  edges: [
    {
      edgeId: 'incoming-review',
      label: '已接受',
      sourceNodeInstanceId: 'events',
      sourcePort: 'accepted',
      targetNodeInstanceId: 'review',
      targetPort: 'in',
      condition: 'nodes.events.ready && event.type in ["business.created", "business.updated"]'
    }
  ]
}

test('editing and JSON roundtrip preserve the complete native definition', () => {
  const edited = cloneGraph(fixture)
  edited.nodes[1].position!.x = 444
  const saved = JSON.parse(JSON.stringify(edited)) as WorkflowGraph
  assert.deepEqual(saved.entryPoints, fixture.entryPoints)
  assert.deepEqual(
    saved.nodes.map((node) => ({ ...node, position: undefined })),
    fixture.nodes.map((node) => ({ ...node, position: undefined }))
  )
  assert.deepEqual(saved.edges, fixture.edges)
  assert.equal(fixture.nodes[1].position?.x, 333)
  const cells = canvasCells(saved, [], { review: 'waiting', loop: 'retrying' })
  const review = cells.find((cell) => cell.id === 'review')
  assert.ok(review && 'data' in review && review.data.executionState === 'waiting')
  assert.equal(review?.shape, 'rect')
  assert.equal(
    review && 'ports' in review ? review.ports.groups.in.attrs.circle.magnet : undefined,
    'passive'
  )
  const incoming = cells.find((cell) => cell.id === 'incoming-review')
  assert.deepEqual(incoming && 'source' in incoming ? incoming.source : undefined, {
    cell: 'events',
    port: 'out:accepted'
  })
})

test('adding defaults and deleting an entry leave other definitions untouched', () => {
  const definition = {
    type: 'test.business.action',
    version: '3.0.0',
    kind: 'action',
    configSchema: {
      properties: {
        list: { default: ['one'] },
        credential: { type: 'string', 'x-coinsphere-secret': true }
      }
    }
  } as unknown as WorkflowNodeDefinition
  const next = addNode(fixture, definition)
  assert.deepEqual(next.nodes.slice(0, -1), fixture.nodes)
  assert.deepEqual(next.nodes.at(-1)?.config, { list: ['one'] })
  const removed = removeNode(next, 'events')
  assert.deepEqual(removed.entryPoints, { manualReview: 'review' })
  assert.equal(removed.edges.length, 0)
  assert.deepEqual(removed.nodes[0].inputBindings, fixture.nodes[1].inputBindings)
})

test('compact rectangular canvas shows short branches and preserves stored positions and expressions', () => {
  const graph = cloneGraph(fixture)
  graph.edges[0].sourcePort = 'unavailable'
  delete graph.edges[0].label
  const before = cloneGraph(graph)
  const cells = canvasCells(graph, [], { review: 'waiting' }, 'review')
  const review = cells.find((cell) => cell.id === 'review')!
  assert.ok('body' in review.attrs && review.attrs.body.strokeWidth === 3)
  assert.ok('width' in review && review.width === 210 && review.height === 70)
  const edge = cells.find((cell) => cell.id === 'incoming-review')!
  assert.ok(
    'router' in edge && edge.router.name === 'manhattan' && edge.connector.name === 'rounded'
  )
  assert.ok('labels' in edge && edge.labels[0] === '数据不可用')
  assert.deepEqual(graph, before)
})

test('Chinese schema labels preserve original binding paths', () => {
  assert.deepEqual(
    schemaFieldPaths({
      properties: {
        metrics: {
          title: '指标',
          properties: { maxDrawdown: { title: '最大回撤', type: 'string' } }
        },
        model: { title: '模型名称', type: 'string' }
      }
    }),
    [
      { value: 'metrics', label: '指标' },
      { value: 'metrics.maxDrawdown', label: '指标 / 最大回撤' },
      { value: 'model', label: '模型名称' }
    ]
  )
})

test('output labels use schema metadata and keep raw values untouched', () => {
  const output = {
    side: 'buy',
    ready: false,
    model: 'GPT-6',
    details: { value: '0.123456789' },
    raw: 'HTTP 429'
  }
  assert.deepEqual(
    schemaOutputFields(
      {
        properties: {
          side: { title: '方向', enum: ['buy', 'sell'], enumLabels: ['买入', '卖出'] },
          ready: { title: '是否就绪' },
          model: { title: '模型名称' },
          details: { title: '详情' }
        }
      },
      output
    ),
    [
      { key: 'side', title: '方向', value: '买入' },
      { key: 'ready', title: '是否就绪', value: '否' },
      { key: 'model', title: '模型名称', value: 'GPT-6' },
      { key: 'details', title: '详情', value: '{"value":"0.123456789"}' }
    ]
  )
  assert.equal(output.side, 'buy')
  assert.equal(output.raw, 'HTTP 429')
  assert.deepEqual(schemaOutputFields({}, output), [])
})

test('URL filters preserve explicit all-status and reject ambiguous identifiers', () => {
  const defaults = { status: 'pending', workflowId: 0 }
  assert.deepEqual(readRouteFilters(defaults, { status: '', workflowId: '7' }), {
    status: '',
    workflowId: 7
  })
  assert.deepEqual(readRouteFilters(defaults, {}), defaults)
  for (const workflowId of [['7', '8'], '-1', '1.2', '1e2', '9007199254740992'])
    assert.throws(() => readRouteFilters(defaults, { workflowId }))
  assert.throws(() => readRouteFilters(defaults, { status: 'x'.repeat(4097) }))
})
