import { readFileSync } from 'node:fs'
import { expect, test, type Page, type Route } from '@playwright/test'
import type { PluginCatalogItem } from '../src/api/pluginCatalog'

const createdAt = '2026-08-01T00:00:00Z'
const accessToken = `header.${Buffer.from(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + 3600 })).toString('base64url')}.signature`
const productionCsp = readFileSync(new URL('../nginx.conf', import.meta.url), 'utf8').match(
  /add_header Content-Security-Policy "([^"]+)" always;/
)?.[1]
const iconifyApiOrigins = [
  'https://api.iconify.design',
  'https://api.unisvg.com',
  'https://api.simplesvg.com'
]
if (!productionCsp || iconifyApiOrigins.some((origin) => !productionCsp.includes(origin)))
  throw new Error('production CSP must permit Iconify')

const graph = {
  schemaVersion: 3,
  entryPoints: { main: 'start' },
  nodes: [
    {
      nodeInstanceId: 'start',
      nodeType: 'core.manual',
      nodeVersion: '1.0.0',
      config: {},
      position: { x: 100, y: 160 }
    },
    {
      nodeInstanceId: 'end',
      nodeType: 'core.end',
      nodeVersion: '1.0.0',
      config: {},
      position: { x: 420, y: 160 }
    }
  ],
  edges: [
    {
      edgeId: 'start-end',
      sourceNodeInstanceId: 'start',
      sourcePort: 'out',
      targetNodeInstanceId: 'end',
      targetPort: 'in'
    }
  ]
}
const nodeDefinitions = ['manual', 'end'].map((type) => ({
  type: `core.${type}`,
  version: '1.0.0',
  title: type === 'manual' ? '手工入口' : '结束',
  kind: type === 'manual' ? 'trigger' : 'action',
  category: 'Core',
  description: '',
  capabilities: { deterministic: true, stateless: true },
  configSchema: {},
  inputSchema: {},
  outputSchema: {},
  uiSchema: {},
  inputPorts: type === 'manual' ? [] : ['in'],
  outputPorts: type === 'end' ? [] : ['out'],
  secretFields: [],
  available: true
}))
nodeDefinitions.push({
  type: 'core.condition',
  version: '1.0.0',
  title: '条件判断',
  kind: 'action',
  category: 'Core',
  description: '',
  capabilities: { deterministic: true, stateless: true },
  configSchema: {
    properties: {
      match: { default: 'all' },
      rules: { default: [{ fieldPath: ['value'], operator: 'true' }] }
    }
  },
  inputSchema: { additionalProperties: {} },
  outputSchema: { properties: { branch: { type: 'string' } } },
  uiSchema: {},
  inputPorts: ['in'],
  outputPorts: ['true', 'false', 'unavailable'],
  secretFields: [],
  available: true
} as (typeof nodeDefinitions)[number])
const menu = (id: number, path: string, component: string, title: string) => ({
  id,
  parentId: null,
  path,
  component,
  name: `TestMenu${id}`,
  updatedAt: createdAt,
  meta: {
    title,
    keepAlive: false,
    isHide: false,
    isHideTab: false,
    isFullPage: false,
    isIframe: false,
    fixedTab: false,
    isEnable: true,
    sort: id
  }
})

type FixtureMenu = Omit<ReturnType<typeof menu>, 'parentId'> & {
  parentId: number | null
  pluginId?: string
  contributionType?: string
  pageKey?: string
  children?: FixtureMenu[]
}

async function fulfillApi(route: Route, data: unknown, status = 200, msg = '') {
  await route.fulfill({
    status,
    contentType: status >= 400 ? 'application/problem+json' : 'application/json',
    body: JSON.stringify(
      status >= 400
        ? {
            type: 'about:blank',
            title: 'Request rejected',
            status,
            detail: msg,
            requestId: 'synthetic'
          }
        : { code: status, msg, data }
    )
  })
}

// Only transport is replaced. All navigation, forms, component loading and state updates use the production UI.
async function backendFixture(
  page: Page,
  options: {
    anonymous?: boolean
    count?: number
    groups?: boolean
    denyWorkflow?: boolean
    business?: boolean
    missingComponent?: boolean
    quant?: boolean
    empty?: boolean
    expired?: boolean
    navigation?: boolean
    ai?: boolean
  } = {}
) {
  page.on('pageerror', (error) => console.error('UI runtime error:', error.message))
  page.on('console', (message) => {
    if (message.type() === 'error' && !message.text().includes('/ws/'))
      console.error('UI console error:', message.text())
  })

  const calls: string[] = [],
    unexpected: string[] = [],
    payloads: unknown[] = []
  let groups = options.groups
    ? [
        { id: 1, name: '策略', sort: 10 },
        { id: 2, name: '通知', sort: 20 }
      ]
    : []
  const workflow = {
    id: 7,
    name: '业务审批示例',
    description: '合成定义',
    groupId: null,
    mode: 'batch',
    status: 'inactive',
    draftRevisionId: 11,
    publishedRevisionId: 0,
    mainTriggerNodeId: 'start',
    retentionDays: 30,
    createdBy: 1,
    createdAt,
    updatedAt: createdAt,
    stateNodeInstanceIds: [],
    runtime: { maxConcurrentRuns: 1, backlogLimit: 20, updatedAt: createdAt }
  }
  let revision = {
    id: 11,
    workflowId: 7,
    revisionNumber: 1,
    graph,
    nodeVersions: {},
    mainTriggerNodeId: 'start',
    createdBy: 1,
    createdAt,
    secretFields: {}
  }
  const revisions = [revision]
  const run = {
    id: 21,
    workflowId: 7,
    revisionId: 11,
    entryPoint: 'main',
    input: {},
    triggerType: 'manual',
    status: 'waiting',
    triggeredAt: createdAt,
    diagnostic: false,
    resultSummary: {},
    runNodes: [],
    logs: [],
    artifacts: []
  }
  let tasks = [
    {
      id: 31,
      workflowId: 7,
      runId: 21,
      nodeInstanceId: 'review',
      taskType: 'approval',
      businessKey: 'case-1',
      prompt: '审核合成业务事项',
      status: 'pending',
      expiresAt: '2099-01-01T00:00:00Z',
      createdAt
    }
  ]
  const view = {
    pluginId: options.missingComponent
      ? 'test.uncompiled'
      : options.business
        ? 'example.business'
        : options.quant
          ? 'official.quant'
          : 'official.binance',
    pageKey: options.business ? 'tasks' : options.quant ? 'backtests' : 'paper',
    allowedActions: options.business ? ['ack'] : [],
    createdAt
  }
  const catalog: PluginCatalogItem[] = [
    {
      id: view.pluginId,
      name: '结果插件',
      version: '5.0.0',
      pages: [],
      headerActions: [],
      resultPages: [
        {
          pageKey: view.pageKey,
          title: '模拟结果',
          componentEntry: 'PaperResultPage',
          configComponentEntry: options.quant
            ? 'BacktestResultConfig.vue'
            : options.business
              ? undefined
              : 'PaperResultConfig.vue',
          scopeSchema: {},
          filterSchema: {
            type: 'object',
            properties: { instrument: { type: 'string', title: '交易对' } }
          },
          actions: options.business ? ['ack'] : []
        }
      ],
      runPanels: options.business
        ? [
            {
              panelKey: 'task',
              title: '业务事项',
              nodeTypes: ['example.business.task'],
              componentEntry: 'TaskPanel.vue'
            }
          ]
        : []
    }
  ]
  const menus: FixtureMenu[] = [
    menu(2, '/scheduler/definition', '/scheduler/workflow/index', '工作流'),
    {
      ...menu(
        3,
        `/plugins/${view.pluginId}/result/${view.pageKey}`,
        `plugin-result:${view.pluginId}/${view.pageKey}`,
        options.business ? '业务事项' : options.quant ? '回测结果' : '模拟交易结果'
      ),
      name: `PluginPage:${view.pluginId}/result/${view.pageKey}`,
      pluginId: view.pluginId,
      contributionType: 'result',
      pageKey: view.pageKey
    },
    menu(4, '/profile', '/system/user-center/index', '个人中心')
  ]
  if (options.navigation)
    menus.push(
      menu(5, '/system/menu', '/system/menu/index', '菜单管理'),
      menu(10, '/menus/10', '/index/index', '自选目录'),
      { ...menu(11, '/menus/11', '/index/index', '分析'), parentId: 10 }
    )
  if (options.ai) {
    catalog.push({
      id: 'official.ai',
      name: '人工智能',
      version: '5.0.0',
      pages: [{ pageKey: 'models', title: '模型配置', icon: 'ri:brain-line' }],
      resultPages: [],
      runPanels: [],
      headerActions: [
        { pluginId: 'official.ai', key: 'chat', title: '智能助手', icon: 'ri:message-3-line' }
      ]
    })
    menus.push({
      ...menu(6, '/plugins/official.ai/page/models', 'plugin:official.ai/models', '模型配置'),
      name: 'PluginPage:official.ai/page/models',
      pluginId: 'official.ai',
      contributionType: 'page',
      pageKey: 'models'
    })
  }
  const menuTree = (parentId: number | null = null): FixtureMenu[] =>
    menus
      .filter((item) => item.parentId === parentId)
      .map((item) => ({ ...item, children: menuTree(item.id) }))
  const model = {
    id: 1,
    displayName: '合成本地模型',
    modelName: 'synthetic',
    baseUrl: 'http://model.internal/v1',
    apiKeyMasked: '********',
    isEnabled: true,
    priority: 100,
    timeoutMs: 60000,
    createdAt,
    updatedAt: createdAt
  }
  const assistantSession = {
    id: 81,
    modelId: 1,
    modelName: 'synthetic',
    title: '合成会话',
    messageCount: 2,
    latestPreview: '插件助手已完成查询',
    createdAt,
    updatedAt: createdAt,
    lastMessageAt: createdAt
  }
  let assistantSessions: (typeof assistantSession)[] = []
  if (!options.anonymous)
    await page.addInitScript(
      (token) => sessionStorage.setItem('coinsphere-access-token', token),
      accessToken
    )
  await page.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (iconifyApiOrigins.includes(url.origin)) {
      const prefix =
        url.pathname
          .split('/')
          .pop()
          ?.replace(/\.json$/, '') || 'ri'
      const names = (url.searchParams.get('icons') || '').split(',').filter(Boolean)
      await route.fulfill({
        contentType: 'application/json',
        headers: { 'access-control-allow-origin': '*' },
        body: JSON.stringify({
          prefix,
          width: 24,
          height: 24,
          icons: Object.fromEntries(
            names.map((name) => [name, { body: '<path fill="currentColor" d="M2 2h20v20H2z" />' }])
          )
        })
      })
      return
    }
    if (!url.pathname.startsWith('/api/')) {
      if (url.origin !== 'http://127.0.0.1:4173') {
        await route.abort()
        return
      }
      await route.continue()
      return
    }
    const path = url.pathname,
      method = route.request().method(),
      key = `${method} ${path}`
    calls.push(key)
    const reply = (data: unknown) => fulfillApi(route, data)
    if (method === 'PUT' && path === '/api/v1/me/password') {
      payloads.push(route.request().postDataJSON())
      await reply({})
      return
    }
    if (path === '/api/v1/me') {
      if (options.anonymous) {
        await fulfillApi(route, null, 401, '请先登录')
        return
      }
      await reply({
        userId: 1,
        username: 'coinsphere',
        email: '',
        avatar: '',
        accessMode: 'authenticated'
      })
      return
    }
    if (path === '/api/v1/system/i18n-dictionaries') {
      await reply({ zh: {}, en: {} })
      return
    }
    if (
      method === 'GET' &&
      (path === '/api/v1/system/menus' || path === '/api/v1/system/menus/manage-tree')
    ) {
      await reply(menuTree())
      return
    }
    if (path === '/api/v1/system/menus/pages') {
      await reply([
        {
          pluginId: view.pluginId,
          contributionType: 'result',
          pageKey: view.pageKey,
          title: '模拟交易结果',
          icon: '',
          menuId: 3
        }
      ])
      return
    }
    if (method === 'PUT' && /^\/api\/v1\/system\/menus\/\d+$/.test(path)) {
      const body = route.request().postDataJSON()
      payloads.push(body)
      const item = menus.find((item) => item.id === Number(path.split('/').pop()))!
      Object.assign(item, {
        parentId: body.parentId,
        pluginId: body.pluginId,
        contributionType: body.contributionType,
        pageKey: body.pageKey
      })
      Object.assign(item.meta, {
        title: body.title,
        icon: body.icon,
        sort: body.sort,
        isHide: body.isHidden,
        isEnable: body.isActive,
        keepAlive: body.keepAlive,
        isHideTab: body.hideTab,
        fixedTab: body.fixedTab
      })
      await reply({ id: item.id })
      return
    }
    if (options.ai && path === '/api/v1/plugins/official.ai/models') {
      await reply([model])
      return
    }
    if (options.ai && path === '/api/v1/plugins/official.ai/assistant/models') {
      await reply([model])
      return
    }
    if (options.ai && path === '/api/v1/plugins/official.ai/assistant/sessions') {
      if (method === 'POST') {
        payloads.push(route.request().postDataJSON())
        assistantSessions = [assistantSession]
        await reply(assistantSession)
      } else await reply(assistantSessions)
      return
    }
    if (options.ai && path === '/api/v1/plugins/official.ai/assistant/sessions/81/stream') {
      payloads.push(route.request().postDataJSON())
      const user = { id: 91, role: 'user', content: '查询平台工作流', createdAt }
      const message = { id: 92, role: 'assistant', content: '插件助手已完成查询', createdAt }
      await route.fulfill({
        contentType: 'text/event-stream',
        body: [
          ['user', { message: user }],
          ['tool', { name: 'platform.summary', status: 'completed' }],
          ['content', { content: '插件助手已完成查询' }],
          ['done', { message, session: assistantSession }]
        ]
          .map(([event, data]) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`)
          .join('')
      })
      return
    }
    if (path === '/api/v1/notification-deliveries') {
      await reply({ records: [], total: 0, hasMore: false, nextCursor: '', unreadCount: 0 })
      return
    }
    if (path === '/api/v1/human-tasks') {
      await reply({ items: tasks })
      return
    }
    if (method === 'PUT' && path === '/api/v1/workflow-groups/order') {
      const body = route.request().postDataJSON()
      payloads.push(body)
      groups = body.groupIds.map((id: number, index: number) => ({
        ...groups.find((group) => group.id === id),
        sort: (index + 1) * 10
      }))
      await reply({ items: groups })
      return
    }
    if (path === '/api/v1/workflow-groups') {
      await reply({ items: groups })
      return
    }
    if (path === '/api/v1/plugins/catalog') {
      await reply({ items: catalog })
      return
    }
    if (path === '/api/v1/plugins/official.binance/results/paper') {
      await reply({
        orders: options.empty
          ? []
          : [
              {
                id: 61,
                runId: 21,
                clientOrderId: 'synthetic-order',
                market: 'spot',
                instrument: 'BTCUSDT',
                side: 'buy',
                executed: '1',
                averagePrice: '100',
                notional: '100',
                status: 'filled',
                createdAt
              }
            ],
        accounts: [
          { id: 'synthetic-paper', cashBalance: '100.00', equity: '100.00', positions: [] }
        ]
      })
      return
    }
    if (path === '/api/v1/plugins/example.business/results/tasks') {
      await reply({ message: '业务事项可读取' })
      return
    }
    if (method === 'POST' && path === '/api/v1/plugins/example.business/results/ack') {
      await reply({ message: '事项已确认' })
      return
    }
    if (path === '/api/v1/plugins/official.quant/results/backtests') {
      await reply({
        items: options.empty
          ? []
          : [
              {
                id: 71,
                workflowId: 7,
                nodeInstanceId: 'backtest',
                runId: 21,
                venue: 'test',
                market: 'spot',
                instrument: 'BTCUSDT',
                interval: '1h',
                initialCapital: '100',
                finalEquity: '110',
                totalReturn: '0.1',
                maxDrawdown: '0.02',
                totalFees: '1',
                tradeCount: 1,
                candleCount: 0,
                startTime: createdAt,
                endTime: createdAt,
                createdAt
              }
            ]
      })
      return
    }
    if (path === '/api/v1/plugins/official.quant/results/backtests/71') {
      await reply({
        schemaVersion: 2,
        venue: 'test',
        market: 'spot',
        instrument: 'BTCUSDT',
        interval: '1h',
        parameters: {},
        candles: [],
        points: []
      })
      return
    }
    if (path === '/api/v1/workflows/node-definitions') {
      await reply({ items: nodeDefinitions })
      return
    }
    if (path === '/api/v1/workflows/templates') {
      await reply({
        items: [{ key: 'blank', name: '空白工作流', mode: 'batch', description: '手工入口与结束' }]
      })
      return
    }
    if (path === '/api/v1/workflows/validate') {
      await reply({ valid: true, issues: [] })
      return
    }
    if (method === 'GET' && path === '/api/v1/workflows') {
      const records = Array.from({ length: options.count ?? 1 }, (_, index) => ({
        ...workflow,
        id: 7 + index,
        name: `业务审批示例 ${index + 1}`,
        latestRunId: 21,
        latestRunStatus: index % 2 ? 'retrying' : 'waiting',
        maxConcurrentRuns: 1,
        backlogLimit: 20
      }))
      await reply({ records, total: records.length, hasMore: false, nextCursor: '' })
      return
    }
    if (method === 'POST' && path === '/api/v1/workflows') {
      const body = route.request().postDataJSON()
      payloads.push(body)
      Object.assign(workflow, { name: body.name, description: body.description })
      revision.graph = body.graph
      await reply(workflow)
      return
    }
    if (method === 'GET' && path === '/api/v1/workflows/7') {
      if (options.denyWorkflow) {
        await fulfillApi(route, null, 404, '工作流不存在')
        return
      }
      await reply(workflow)
      return
    }
    if (method === 'GET' && path === '/api/v1/workflows/7/revisions') {
      await reply({ items: revisions })
      return
    }
    if (method === 'GET' && /^\/api\/v1\/workflows\/7\/revisions\/\d+$/.test(path)) {
      await reply(revisions.find((item) => item.id === Number(path.split('/').pop())))
      return
    }
    if (method === 'POST' && path === '/api/v1/workflows/7/revisions') {
      const body = route.request().postDataJSON()
      payloads.push(body)
      revision = { ...revision, id: 12, revisionNumber: 2, graph: body.graph }
      revisions.unshift(revision)
      Object.assign(workflow, body.metadata, { draftRevisionId: revision.id })
      await reply(revision)
      return
    }
    if (method === 'POST' && path === '/api/v1/workflows/7/publish') {
      const body = route.request().postDataJSON()
      payloads.push(body)
      workflow.publishedRevisionId = body.revisionId
      await reply(workflow)
      return
    }
    if (method === 'POST' && path === '/api/v1/workflows/7/runs') {
      const body = route.request().postDataJSON()
      payloads.push(body)
      Object.assign(run, body)
      await reply(run)
      return
    }
    if (method === 'GET' && path === '/api/v1/workflow-runs/21') {
      await reply(
        options.business
          ? {
              ...run,
              status: 'succeeded',
              runNodes: [
                {
                  id: 51,
                  nodeInstanceId: 'task',
                  nodeType: 'example.business.task',
                  nodeVersion: '1.0.0',
                  status: 'succeeded',
                  executionPool: 'compute',
                  attempt: 1,
                  loopIteration: 0,
                  operationKey: 'synthetic-business',
                  inputSummary: {},
                  outputSummary: { message: '业务事项可读取' }
                }
              ]
            }
          : run
      )
      return
    }
    if (method === 'POST' && path === '/api/v1/human-tasks/31') {
      const body = route.request().postDataJSON()
      payloads.push(body)
      if (options.expired) {
        await fulfillApi(route, null, 409, '待办已过期')
        return
      }
      tasks = []
      await reply({ id: 31, status: body.action === 'approve' ? 'approved' : 'rejected' })
      return
    }
    unexpected.push(key)
    await fulfillApi(route, null, 500, `unhandled fixture: ${key}`)
  })
  return { calls, unexpected, payloads, workflow }
}

test('三级菜单移动和改名立即刷新导航与已有标签，刷新后地址不变', async ({ page }) => {
  const backend = await backendFixture(page, { navigation: true })
  const stablePath = '/plugins/official.binance/result/paper'
  await page.goto(stablePath)
  await expect(page.getByRole('heading', { name: '模拟交易结果', exact: true })).toBeVisible()
  await page.getByRole('menuitem').filter({ hasText: '菜单管理' }).click()
  await page
    .locator('.menu-page .el-table__row')
    .filter({ hasText: '模拟交易结果' })
    .getByRole('button', { name: '编辑', exact: true })
    .click()
  const dialog = page.getByRole('dialog', { name: '编辑菜单' })
  await dialog.getByLabel('菜单名称', { exact: true }).fill('我的 Paper')
  await dialog
    .locator('.el-form-item')
    .filter({ hasText: '父级菜单' })
    .locator('.el-select')
    .click()
  await page.getByRole('option').filter({ hasText: '分析' }).click()
  await dialog.getByRole('spinbutton').fill('5')
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.locator('li[id^="scroll-li-"]').filter({ hasText: '我的 Paper' })).toBeVisible()
  await page.locator('.el-sub-menu__title').filter({ hasText: '自选目录' }).click()
  await page.locator('.el-sub-menu__title').filter({ hasText: '分析' }).click()
  await page.locator('.el-menu-item').filter({ hasText: '我的 Paper' }).click()
  await expect(page).toHaveURL(new RegExp(`${stablePath}$`))
  await expect(page.getByRole('navigation', { name: 'breadcrumb' })).toContainText('自选目录')
  await expect(page.getByRole('navigation', { name: 'breadcrumb' })).toContainText('分析')
  await expect(page.getByRole('navigation', { name: 'breadcrumb' })).toContainText('我的 Paper')
  await page.reload()
  await expect(page.getByRole('heading', { name: '模拟交易结果', exact: true })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'breadcrumb' })).toContainText('我的 Paper')
  expect(backend.payloads).toContainEqual(
    expect.objectContaining({
      parentId: 11,
      title: '我的 Paper',
      sort: 5,
      pluginId: 'official.binance',
      contributionType: 'result',
      pageKey: 'paper'
    })
  )
  expect(backend.unexpected).toEqual([])
})

test('隐藏菜单立即移除导航入口并保留页面直接访问', async ({ page }) => {
  const backend = await backendFixture(page, { navigation: true })
  await page.goto('/system/menu')
  await page
    .locator('.menu-page .el-table__row')
    .filter({ hasText: '模拟交易结果' })
    .getByRole('button', { name: '编辑', exact: true })
    .click()
  const dialog = page.getByRole('dialog', { name: '编辑菜单' })
  await dialog
    .locator('.el-form-item')
    .filter({ hasText: '隐藏菜单' })
    .locator('.el-switch')
    .click()
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByRole('menuitem').filter({ hasText: '模拟交易结果' })).toHaveCount(0)
  await page.goto('/plugins/official.binance/result/paper')
  await expect(page.getByRole('heading', { name: '模拟交易结果', exact: true })).toBeVisible()
  await expect(page.getByRole('menuitem').filter({ hasText: '模拟交易结果' })).toHaveCount(0)
  expect(backend.payloads).toContainEqual(expect.objectContaining({ isHidden: true }))
  expect(backend.unexpected).toEqual([])
})

test('AI 插件提供模型页和可键盘打开的流式助手', async ({ page }) => {
  const backend = await backendFixture(page, { ai: true })
  await page.goto('/plugins/official.ai/page/models')
  await expect(page.getByText('合成本地模型', { exact: true })).toBeVisible()
  const entry = page.getByRole('button', { name: '智能助手', exact: true })
  await entry.focus()
  await entry.press('Enter')
  await expect(page.locator('.assistant-header h2')).toBeVisible()
  await page.locator('.assistant-composer textarea').fill('查询平台工作流')
  await page.locator('.assistant-composer').getByRole('button').click()
  await expect(
    page.locator('.assistant-message__bubble').filter({ hasText: '插件助手已完成查询' })
  ).toBeVisible()
  await expect(page.locator('.tool-status')).toContainText('platform.summary')
  expect(backend.payloads).toContainEqual({ modelId: 1 })
  expect(backend.payloads).toContainEqual({ text: '查询平台工作流' })

  await page.route('**/api/v1/plugins/official.ai/assistant/sessions/81/stream', (route) =>
    fulfillApi(route, null, 404, '助手会话不存在')
  )
  await page.locator('.assistant-composer textarea').fill('查询失效会话')
  await page.locator('.assistant-composer').getByRole('button').click()
  await expect(page.locator('.el-message--error')).toContainText('助手会话不存在')
  expect(backend.calls.some((call) => call.includes('/api/v1/assistant/'))).toBe(false)
  expect(backend.unexpected).toEqual([])
})

test('未加载 AI 插件时顶栏不展示聊天入口，直接访问显示插件错误', async ({ page }) => {
  const backend = await backendFixture(page)
  await page.goto('/scheduler/definition')
  await expect(page.getByText('共 1 个工作流')).toBeVisible()
  await expect(page.getByRole('button', { name: '智能助手', exact: true })).toHaveCount(0)
  await page.goto('/plugins/official.ai/page/models')
  await expect(page.locator('.el-alert')).toContainText('插件页面不可用')
  expect(backend.calls.some((call) => call.includes('/plugins/official.ai/'))).toBe(false)
  expect(backend.unexpected).toEqual([])
})

test('匿名直接访问编辑器被登录边界拦截', async ({ page }) => {
  const backend = await backendFixture(page, { anonymous: true })
  await page.goto('/scheduler/workflow/create')
  await expect(page).toHaveURL(/\/auth\/login\?redirect=/)
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()
  expect(backend.calls.filter((call) => call.includes('/workflows'))).toEqual([])
})

test('作者完成创建、保存、发布和手工运行', async ({ page }) => {
  const backend = await backendFixture(page)
  await page.goto('/scheduler/workflow/create')
  await page.getByRole('button', { name: '基础信息', exact: true }).click()
  await page.locator('.workflow-meta input').first().fill('合成业务流程')
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect(page).toHaveURL(/\/scheduler\/workflow\/7\/edit$/)
  await expect(page.getByText('草稿已保存', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '发布草稿', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '基础信息', exact: true }).click()
  await expect(page.getByText(/草稿\s*#11\s*·\s*已发布\s*暂无/)).toBeVisible()
  await page.locator('.workflow-meta input').first().fill('修改后的业务流程')
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect(page.getByText(/草稿\s*#12\s*·\s*已发布\s*暂无/)).toBeVisible()
  expect(backend.workflow.publishedRevisionId).toBe(0)
  await page.getByRole('button', { name: '发布草稿', exact: true }).click()
  await expect(page.getByText(/草稿\s*#12\s*·\s*已发布\s*#12/)).toBeVisible()
  await page.getByRole('button', { name: '运行', exact: true }).click()
  await page.getByRole('button', { name: '加入运行队列' }).click()
  await expect(page).toHaveURL(/\/scheduler\/execution\/21\/detail$/)
  await expect(page.getByRole('heading', { name: '运行 #21' })).toBeVisible()
  await expect(page.locator('.run-header .el-tag').first()).toHaveText('等待处理')
  expect(backend.payloads).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ expectedDraftRevisionId: 11, graph }),
      { revisionId: 12, expectedPublishedRevisionId: 0 },
      { revisionId: 12, entryPoint: 'main', input: {} }
    ])
  )
  expect(backend.unexpected).toEqual([])
})

for (const action of ['approve', 'reject'] as const)
  test(`运行详情审批 ${action} 后更新待办`, async ({ page }) => {
    const backend = await backendFixture(page)
    await page.goto('/scheduler/execution/21/detail')
    await expect(page.getByText('审核合成业务事项')).toBeVisible()
    await page
      .getByRole('button', { name: action === 'approve' ? '通过' : '拒绝', exact: true })
      .click()
    await page.getByRole('button', { name: '确定', exact: true }).click()
    await expect(page.getByText('审核合成业务事项')).toHaveCount(0)
    expect(backend.payloads).toContainEqual({ action, data: {} })
    expect(backend.unexpected).toEqual([])
  })

test('过期审批返回可见错误并保留运行详情', async ({ page }) => {
  const backend = await backendFixture(page, { expired: true })
  await page.goto('/scheduler/execution/21/detail')
  await page.getByRole('button', { name: '通过', exact: true }).click()
  await page.getByRole('button', { name: '确定', exact: true }).click()
  await expect(page.getByText('待办已过期').first()).toBeVisible()
  await expect(page).toHaveURL(/execution\/21\/detail/)
  expect(backend.unexpected).toEqual([])
})

test('插件菜单默认展示模拟交易结果并支持筛选刷新和运行关联', async ({ page }) => {
  const backend = await backendFixture(page)
  await page.goto('/scheduler/definition')
  await page.getByRole('menuitem').filter({ hasText: '模拟交易结果' }).click()
  await expect(page.getByRole('heading', { name: '模拟交易结果', exact: true })).toBeVisible()
  await expect(page.getByText('synthetic-order')).toBeVisible()
  await expect(page.getByRole('button', { name: '创建结果视图' })).toHaveCount(0)
  const filter = page.getByLabel('交易对', { exact: true })
  await filter.fill('BTCUSDT')
  const filtered = page.waitForRequest(
    (r) => r.url().includes('/results/paper') && r.url().includes('instrument=BTCUSDT')
  )
  await page.getByRole('button', { name: '筛选', exact: true }).click()
  await filtered
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await page.getByRole('button', { name: '#21', exact: true }).click()
  await expect(page).toHaveURL(/execution\/21\/detail/)
  expect(backend.calls.some((c) => c.includes('result-views'))).toBe(false)
  expect(backend.calls.some((c) => c.includes('workbench'))).toBe(false)
  expect(backend.unexpected).toEqual([])
})

test('量化菜单直接查询并复用回测分析', async ({ page }) => {
  const backend = await backendFixture(page, { quant: true })
  await page.goto('/plugins/official.quant/result/backtests')
  await expect(page.getByRole('heading', { name: '回测结果', exact: true })).toBeVisible()
  await page.getByLabel('交易对', { exact: true }).fill('BTCUSDT')
  const filtered = page.waitForRequest(
    (r) => r.url().includes('/results/backtests?') && r.url().includes('instrument=BTCUSDT')
  )
  await page.getByRole('button', { name: '筛选', exact: true }).click()
  await filtered
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await page.getByRole('button', { name: '详情', exact: true }).click()
  await expect(page.getByRole('heading', { name: '回测摘要', exact: true })).toHaveCount(0)
  await expect(page.getByLabel('回测摘要')).toBeVisible()
  expect(backend.calls).toContain('GET /api/v1/plugins/official.quant/results/backtests/71')
  expect(backend.unexpected).toEqual([])
})

for (const quant of [false, true])
  test(`插件结果空数据 ${quant ? '量化' : '币安'}`, async ({ page }) => {
    const backend = await backendFixture(page, { quant, empty: true })
    await page.goto(
      quant ? '/plugins/official.quant/result/backtests' : '/plugins/official.binance/result/paper'
    )
    await expect(page.getByText(quant ? '暂无回测结果' : '暂无 Paper 订单')).toBeVisible()
    expect(backend.unexpected).toEqual([])
  })

test('不存在的工作流显示资源错误', async ({ page }) => {
  await backendFixture(page, { denyWorkflow: true })
  await page.goto('/scheduler/workflow/7/edit', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('.el-alert')).toContainText('工作流不存在')
})

test('列表读取不随工作流数量增加，并展示真实等待和重试状态', async ({ page }) => {
  const backend = await backendFixture(page, { count: 25 })
  await page.goto('/scheduler/definition')
  await expect(page.getByText('共 25 个工作流')).toBeVisible()
  await expect(
    page.locator('.workflow-library .el-table').getByText('等待处理').first()
  ).toBeVisible()
  await expect(
    page.locator('.workflow-library .el-table').getByText('等待重试').first()
  ).toBeVisible()
  expect(backend.calls.filter((call) => call === 'GET /api/v1/workflows')).toHaveLength(1)
  expect(backend.calls.filter((call) => /^GET \/api\/v1\/workflows\/\d+/.test(call))).toEqual([])
  expect(backend.unexpected).toEqual([])
})

test('普通业务插件通过所属菜单和运行面板加载', async ({ page }) => {
  const backend = await backendFixture(page, { business: true })
  await page.goto('/plugins/example.business/result/tasks')
  await expect(page.getByLabel('业务事项结果')).toContainText('业务事项可读取')
  await page.getByRole('button', { name: '确认事项', exact: true }).click()
  await expect(page.getByLabel('业务事项结果')).toContainText('事项已确认')
  expect(backend.calls).toContain('POST /api/v1/plugins/example.business/results/ack')
  await page.goto('/scheduler/execution/21/detail')
  await page.getByRole('tab', { name: '节点记录', exact: true }).click()
  await page.getByRole('cell', { name: 'task', exact: true }).click()
  await page.getByRole('tab', { name: '业务事项', exact: true }).click()
  await expect(page.getByLabel('业务节点结果')).toContainText('业务事项可读取')
  expect(backend.unexpected).toEqual([])
})

test('插件缺少前端贡献时给出可见反馈', async ({ page }) => {
  const backend = await backendFixture(page, {
    missingComponent: true
  })
  await page.goto('/plugins/test.uncompiled/result/paper')
  await expect(page.locator('.plugin-result-page .el-alert')).toContainText(
    '插件 test.uncompiled 的前端组件未编译'
  )
  expect(backend.unexpected).toEqual([])
})

test('窄屏通过键盘操作主要按钮', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const backend = await backendFixture(page)
  await page.goto('/scheduler/workflow/create')
  await page.getByRole('button', { name: '基础信息', exact: true }).click()
  await page.locator('.workflow-meta input').first().fill('键盘流程')
  await page.locator('.meta-popover').getByRole('button', { name: '应用', exact: true }).click()
  const save = page.getByRole('button', { name: '保存草稿', exact: true })
  await save.focus()
  await expect(save).toBeFocused()
  const bounds = await save.boundingBox()
  expect(bounds && bounds.x >= 0 && bounds.x + bounds.width <= 390).toBeTruthy()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/scheduler\/workflow\/7\/edit$/)
  expect(backend.unexpected).toEqual([])
})

test('生产 CSP 允许登录页渲染', async ({ page }) => {
  const iconifyRequests: string[] = []
  await page.route('**/*', async (route) => {
    const url = new URL(route.request().url())
    if (iconifyApiOrigins.includes(url.origin)) {
      iconifyRequests.push(url.href)
      const prefix =
        url.pathname
          .split('/')
          .pop()
          ?.replace(/\.json$/, '') || 'ri'
      const names = (url.searchParams.get('icons') || '').split(',').filter(Boolean)
      await route.fulfill({
        contentType: 'application/json',
        headers: { 'access-control-allow-origin': '*' },
        body: JSON.stringify({
          prefix,
          width: 24,
          height: 24,
          icons: Object.fromEntries(
            names.map((name) => [name, { body: '<path fill="currentColor" d="M2 2h20v20H2z" />' }])
          )
        })
      })
      return
    }
    if (url.origin !== 'http://127.0.0.1:4173') {
      await route.abort()
      return
    }
    const response = await route.fetch()
    await route.fulfill({
      response,
      headers: { ...response.headers(), 'content-security-policy': productionCsp }
    })
  })
  await page.goto('/auth/login')
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()
  await expect(page.locator('.palette-btn svg.art-svg-icon')).toBeVisible()
  expect(iconifyRequests.length).toBeGreaterThan(0)
})

test('旧版画布支持素材、节点编辑、撤销重做和保存原生定义', async ({ page }) => {
  const backend = await backendFixture(page)
  await page.goto('/scheduler/workflow/7/edit', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('.editor-toolbar')).toBeVisible()
  await expect(page.locator('.x6-node')).toHaveCount(2)
  await page.getByRole('button', { name: '结束', exact: true }).click()
  await expect(page.locator('.x6-node')).toHaveCount(3)
  await page.getByRole('button', { name: '撤销', exact: true }).click()
  await expect(page.locator('.x6-node')).toHaveCount(2)
  await page.getByRole('button', { name: '重做', exact: true }).click()
  await expect(page.locator('.x6-node')).toHaveCount(3)
  await page.getByRole('button', { name: '素材面板', exact: true }).click()
  await expect(page.getByRole('complementary', { name: '节点库' })).toHaveCount(0)
  await page.locator('.x6-node').last().click()
  await expect(page.getByRole('complementary', { name: '节点与连线编辑' })).toBeVisible()
  await page.getByRole('button', { name: '删除节点', exact: true }).click()
  await expect(page.locator('.x6-node')).toHaveCount(2)
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  expect(backend.payloads).toContainEqual(expect.objectContaining({ graph }))
  expect(backend.unexpected).toEqual([])
})

test('运行画布展示待审批并在决定后刷新检查面板', async ({ page }) => {
  const backend = await backendFixture(page)
  await page.goto('/scheduler/execution/21/detail')
  await expect(page.getByRole('region', { name: '实时运行日志' })).toBeVisible()
  await expect(page.getByRole('complementary', { name: '运行检查面板' })).toBeVisible()
  await expect(page.getByText('审核合成业务事项')).toBeVisible()
  await page.getByRole('button', { name: '通过', exact: true }).click()
  await page.getByRole('button', { name: '确定', exact: true }).click()
  await expect(page.getByText('审核合成业务事项')).toHaveCount(0)
  expect(backend.payloads).toContainEqual({ action: 'approve', data: {} })
  expect(backend.unexpected).toEqual([])
})

test('分组侧栏筛选和排序使用当前生命周期接口', async ({ page }) => {
  const backend = await backendFixture(page, {
    groups: true
  })
  await page.goto('/scheduler/definition')
  await expect(page.getByRole('complementary', { name: '工作流分组' })).toBeVisible()
  const filtered = page.waitForRequest(
    (request) =>
      request.url().includes('/api/v1/workflows?') &&
      new URL(request.url()).searchParams.get('groupId') === '2'
  )
  await page.getByRole('button', { name: '通知', exact: true }).click()
  await filtered
  await expect(page.getByRole('button', { name: '通知', exact: true })).toHaveClass(/is-active/)
  await page.getByRole('button', { name: '管理分组 通知', exact: true }).click()
  await page.getByRole('menuitem', { name: '上移', exact: true }).click()
  await expect.poll(() => backend.payloads).toContainEqual({ groupIds: [2, 1] })
  expect(backend.unexpected).toEqual([])
})

test('个人中心修改密码后清理会话并返回登录页', async ({ page }) => {
  const backend = await backendFixture(page)
  await page.goto('/profile')
  await page.locator('input[autocomplete="current-password"]').fill('synthetic-current')
  const newFields = page.locator('input[autocomplete="new-password"]')
  await newFields.nth(0).fill('synthetic-new')
  await newFields.nth(1).fill('synthetic-new')
  await page.getByRole('button', { name: '修改密码', exact: true }).click()
  await expect(page).toHaveURL(/\/auth\/login/)
  expect(backend.payloads).toContainEqual({
    currentPassword: 'synthetic-current',
    newPassword: 'synthetic-new'
  })
  expect(backend.unexpected).toEqual([])
})

test('固定属性面板编辑条件、输入与连线，保存发布保留原图字段', async ({ page }) => {
  const backend = await backendFixture(page)
  await page.goto('/scheduler/workflow/7/edit')
  await expect(page.getByRole('complementary', { name: '节点与连线编辑' })).toBeVisible()
  await page.getByRole('button', { name: '条件判断', exact: true }).click()
  await page.locator('.x6-node').last().click()
  await expect(page.getByText('全部满足', { exact: true })).toBeVisible()
  await expect(page.locator('.x6-port-label').filter({ hasText: '数据不可用' })).toBeVisible()
  await page.getByRole('tab', { name: '输入绑定', exact: true }).click()
  const input = page.locator('.binding-row').first()
  await input.locator('.el-select').first().click()
  await page.getByRole('option', { name: '固定值', exact: true }).click()
  await input.getByPlaceholder('JSON 值').fill('true')
  await input.getByPlaceholder('JSON 值').press('Tab')
  await page.getByRole('button', { name: '放大', exact: true }).click()
  const transform = await page.locator('.x6-graph-svg-viewport').getAttribute('transform')
  await page.getByLabel('节点名称', { exact: true }).fill('输入判断')
  await expect(page.locator('.x6-graph-svg-viewport')).toHaveAttribute('transform', transform!)
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  const saved = backend.payloads.find((payload: any) => payload.graph?.nodes.length === 3) as any
  expect(saved.graph.nodes.slice(0, 2)).toEqual(graph.nodes)
  expect(saved.graph.edges).toEqual(graph.edges)
  expect(saved.graph.nodes[2]).toEqual(
    expect.objectContaining({
      label: '输入判断',
      config: { match: 'all', rules: [{ fieldPath: ['value'], operator: 'true' }] },
      inputBindings: { value: { kind: 'literal', value: true } }
    })
  )
  expect(backend.unexpected).toEqual([])
})

test('拖动连线与撤销重做保存真实画布状态', async ({ page }) => {
  const backend = await backendFixture(page)
  await page.goto('/scheduler/workflow/7/edit')
  const node = page.locator('.x6-node[data-cell-id="end"]')
  await expect(node).toBeVisible()
  const bounds = await node.boundingBox()
  if (!bounds) throw new Error('结束节点不可见')
  await page.mouse.move(bounds.x + bounds.width / 2, bounds.y + bounds.height / 2)
  await page.mouse.down()
  await page.mouse.move(bounds.x + bounds.width / 2 + 90, bounds.y + bounds.height / 2 + 90, {
    steps: 10
  })
  await page.mouse.up()
  await page.getByRole('button', { name: '撤销', exact: true }).click()
  await page.getByRole('button', { name: '重做', exact: true }).click()
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  const moved = backend.payloads.find(
    (p: any) => p.graph?.nodes.find((n: any) => n.nodeInstanceId === 'end').position.y !== 160
  ) as any
  expect(moved).toBeTruthy()
  expect(moved.graph.edges).toEqual(graph.edges)
  await page.getByRole('button', { name: '条件判断', exact: true }).click()
  const condition = page.locator('.x6-node').last()
  await condition.click()
  const source = condition.locator('circle[port="out:true"]')
  const target = page.locator('.x6-node[data-cell-id="end"] circle[port="in:in"]')
  await expect(source).toBeVisible()
  await expect(target).toBeVisible()
  const from = await source.boundingBox(),
    to = await target.boundingBox()
  if (!from || !to) throw new Error('连线端口不可见')
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 10 })
  await page.mouse.up()
  await expect(page.locator('.x6-edge')).toHaveCount(2)
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  const connected = backend.payloads.find((p: any) => p.graph?.edges.length === 2) as any
  expect(connected.graph.edges[1]).toEqual(
    expect.objectContaining({ sourcePort: 'true', targetPort: 'in', targetNodeInstanceId: 'end' })
  )
  expect(backend.unexpected).toEqual([])
})
