import assert from 'node:assert/strict'
import { test } from 'node:test'
import { addNode, canvasCells, cloneGraph, removeNode } from './graph'
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
