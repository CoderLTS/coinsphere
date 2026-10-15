<template>
  <div class="plugin-page art-full-height" v-loading="loading">
    <ArtSearchBar
      v-model="formFilters"
      :items="formItems"
      :show-expand="false"
      @reset="handleReset"
      @search="handleSearch"
    />

    <ElAlert
      v-if="queryError || error"
      :title="queryError || error"
      type="error"
      :closable="false"
    />
    <div class="plugin-toolbar">
      <div class="plugin-summary" aria-live="polite">
        <strong>{{ filteredPlugins.length }}</strong>
        <span>个插件</span>
        <i></i>
        <span>{{ filteredNodeCount }} 个节点</span>
        <i></i>
        <span>{{ filteredPageCount }} 个页面</span>
      </div>
      <ElTooltip content="刷新插件列表" placement="top">
        <ElButton
          circle
          :icon="Refresh"
          :loading="loading"
          aria-label="刷新插件列表"
          @click="loadPlugins"
        />
      </ElTooltip>
    </div>

    <div v-if="selectedPlugin" class="plugin-workspace">
      <nav class="plugin-list" aria-label="插件目录">
        <button
          v-for="plugin in filteredPlugins"
          :key="plugin.id"
          type="button"
          :class="['plugin-row', { 'is-selected': selectedPlugin.id === plugin.id }]"
          :style="pluginStyle(plugin)"
          :aria-pressed="selectedPlugin.id === plugin.id"
          @click="selectedPluginId = plugin.id"
        >
          <span class="plugin-mark">
            <ArtSvgIcon :icon="pluginVisual(plugin).icon" />
          </span>
          <span class="plugin-copy">
            <strong>{{ pluginLabel(plugin) }}</strong>
            <small>{{ plugin.nodes.length }} 个节点 / {{ plugin.pages.length }} 个页面</small>
          </span>
        </button>
      </nav>

      <section class="plugin-detail" :style="pluginStyle(selectedPlugin)" aria-label="插件详情">
        <header class="plugin-detail__header">
          <div class="plugin-detail__title">
            <h2>{{ pluginLabel(selectedPlugin) }}</h2>
            <span class="plugin-version">第 {{ selectedPlugin.version }} 版</span>
            <ElTag
              :type="selectedPlugin.loaded ? 'success' : 'warning'"
              effect="plain"
              size="small"
              >{{ selectedPlugin.loaded ? '已加载' : '不可用' }}</ElTag
            >
          </div>
          <div class="plugin-capabilities" aria-label="扩展能力">
            <span v-for="item in selectedPlugin.contributes" :key="item">
              <ArtSvgIcon :icon="contributionIcon(item)" />
              {{ contributionLabel(item) }}
            </span>
          </div>
        </header>
        <ElTabs v-model="detailTab" class="plugin-detail__tabs">
          <ElTabPane label="概览" name="overview">
            <p class="plugin-purpose">{{
              pluginPurposes[selectedPlugin.id] ||
              selectedPlugin.nodes[0]?.description ||
              '扩展工作流与业务页面。'
            }}</p>
            <dl class="plugin-overview">
              <div
                ><dt>可用状态</dt
                ><dd>{{
                  selectedPlugin.loaded ? '可用于工作流' : '当前不可用，请查看诊断'
                }}</dd></div
              >
              <div
                ><dt>工作流节点</dt><dd>{{ selectedPlugin.nodes.length }} 个</dd></div
              >
              <div
                ><dt>业务页面</dt><dd>{{ selectedPlugin.pages.length }} 个</dd></div
              >
            </dl>
            <ElButton
              v-if="selectedPlugin.pages.length"
              type="primary"
              plain
              @click="detailTab = 'pages'"
              >查看页面入口</ElButton
            >
            <ElButton v-if="selectedPlugin.nodes.length" @click="detailTab = 'nodes'"
              >查看节点</ElButton
            >
          </ElTabPane>
          <ElTabPane name="pages">
            <template #label>
              <span class="detail-tab-label">
                页面
                <small>{{ selectedPlugin.pages.length }}</small>
              </span>
            </template>

            <div v-if="selectedPlugin.pages.length" class="page-list">
              <div
                v-for="page in selectedPlugin.pages"
                :key="`${page.kind}:${page.pageKey}`"
                class="detail-row"
              >
                <ArtSvgIcon icon="ri:layout-grid-line" />
                <strong>{{ displayText(page.title, '扩展页面') }}</strong>
                <ElTag size="small" effect="plain">{{ pageKindLabel(page.kind) }}</ElTag>
                <ElText size="small">{{ mountedPage(page) ? '已挂载到菜单' : '尚未挂载' }}</ElText>
                <ElButton
                  link
                  type="primary"
                  :disabled="!selectedPlugin.loaded"
                  @click="openPage(page)"
                  >打开页面</ElButton
                >
                <ElButton link @click="router.push('/system/menu')">管理菜单</ElButton>
              </div>
            </div>
            <ElEmpty v-else :image-size="64" description="未注册页面" />
          </ElTabPane>
          <ElTabPane name="nodes">
            <template #label>
              <span class="detail-tab-label">
                节点
                <small>{{ selectedPlugin.nodes.length }}</small>
              </span>
            </template>

            <div v-if="selectedPlugin.nodes.length" :key="selectedPlugin.id" class="node-browser">
              <div class="node-navigation">
                <ElInput
                  v-model="nodeKeyword"
                  :prefix-icon="Search"
                  clearable
                  placeholder="搜索节点"
                  aria-label="搜索节点"
                  class="node-search"
                />
                <nav class="node-list" aria-label="注册节点">
                  <button
                    v-for="node in filteredNodes"
                    :key="node.type"
                    type="button"
                    :class="['node-row', { 'is-selected': selectedNode?.type === node.type }]"
                    :aria-pressed="selectedNode?.type === node.type"
                    @click="selectedNodeType = node.type"
                  >
                    <span class="node-copy">
                      <strong>{{ displayText(node.title, '扩展节点') }}</strong>
                      <small>
                        {{ nodeKindLabel(node.kind) }} / {{ configFields(node).length }} 项参数
                      </small>
                    </span>
                    <ArtSvgIcon icon="ri:arrow-right-s-line" />
                  </button>
                </nav>
              </div>

              <section
                v-if="selectedNode"
                :key="selectedNode.type"
                class="node-config"
                tabindex="0"
                aria-labelledby="plugin-node-title"
              >
                <header class="node-config__header">
                  <p class="node-location">{{ pluginLabel(selectedPlugin) }} / 注册节点</p>
                  <div class="node-config__title">
                    <h3 id="plugin-node-title">{{
                      displayText(selectedNode.title, '扩展节点')
                    }}</h3>
                    <ElTag size="small" effect="plain">{{
                      nodeKindLabel(selectedNode.kind)
                    }}</ElTag>
                  </div>
                  <span class="node-version">第 {{ selectedNode.version }} 版</span>
                </header>
                <div class="node-config__heading">
                  <h4>配置参数</h4>
                  <small>{{ configFields(selectedNode).length }} 项</small>
                </div>
                <dl v-if="configFields(selectedNode).length" class="parameter-list">
                  <div
                    v-for="[fieldName, schema] in configFields(selectedNode)"
                    :key="fieldName"
                    class="parameter-row"
                  >
                    <dt>
                      <strong>{{ schemaTitle(schema, fieldName) }}</strong>
                      <span class="parameter-tags">
                        <span>{{ schemaTypeLabel(schema.type) }}</span>
                        <ElTag
                          v-if="isConfigRequired(selectedNode, fieldName)"
                          size="small"
                          type="warning"
                          effect="plain"
                        >
                          必填
                        </ElTag>
                        <ElTag
                          v-if="schema['x-coinsphere-secret'] === true"
                          size="small"
                          type="danger"
                          effect="plain"
                        >
                          密钥
                        </ElTag>
                      </span>
                    </dt>
                    <dd>
                      <p v-if="displayText(schema.description)" class="parameter-description">
                        {{ displayText(schema.description) }}
                      </p>
                      <p v-if="schemaDefault(schema)" class="parameter-meta">
                        <span>默认值</span>
                        <span>{{ schemaDefault(schema) }}</span>
                      </p>
                      <p v-if="schemaOptions(schema)" class="parameter-meta">
                        <span>可选值</span>
                        <span>{{ schemaOptions(schema) }}</span>
                      </p>
                    </dd>
                  </div>
                </dl>
                <ElEmpty v-else :image-size="64" description="无需配置参数" />
              </section>
              <ElEmpty v-else :image-size="64" description="没有符合条件的节点" />
            </div>
            <ElEmpty v-else :image-size="64" description="未注册节点" />
          </ElTabPane>

          <ElTabPane label="诊断" name="diagnostics">
            <p class="plugin-installation-facts"
              >{{ selectedPlugin.installed ? '已安装' : '未安装或已停用' }} ·
              {{ selectedPlugin.compiled ? `已编译 v${selectedPlugin.compiledVersion}` : '未编译' }}
              · {{ selectedPlugin.loaded ? '已加载' : '未加载' }}</p
            >
            <ElAlert
              v-if="selectedPlugin.reason"
              :title="unavailableReasons[selectedPlugin.reason] || '插件尚未就绪'"
              type="warning"
              :closable="false"
            />

            <dl class="plugin-overview"
              ><div
                ><dt>插件标识</dt><dd>{{ selectedPlugin.id }}</dd></div
              ><div
                ><dt>运行版本</dt><dd>{{ selectedPlugin.version }}</dd></div
              ></dl
            >
          </ElTabPane>
        </ElTabs>
      </section>
    </div>
    <ElEmpty v-else-if="!loading" description="没有符合条件的插件" />
  </div>
</template>

<script setup lang="ts">
  import { Refresh, Search } from '@element-plus/icons-vue'
  import { fetchGetInstalledPlugins, fetchMenuPages, type MenuPageOption } from '@/api/system'
  import { useRouteFilters } from '@/plugins/ui'

  defineOptions({ name: 'Plugins' })

  type InstalledPlugin = Api.System.InstalledPlugin
  const unavailableReasons: Record<string, string> = {
    not_installed: '插件未安装或已停用，请由管理员维护安装状态。',
    not_compiled: '当前应用未包含此插件，请使用包含该插件的版本。',
    not_loaded: '当前应用未加载此插件，请检查依赖与安装版本。',
    version_mismatch: '安装版本与当前应用不一致，请切换匹配版本。'
  }
  type PluginNode = InstalledPlugin['nodes'][number]
  type ConfigSchema = Record<string, any>

  const loading = ref(false)
  const plugins = ref<InstalledPlugin[]>([])
  const selectedPluginId = ref<string>()
  const selectedNodeType = ref<string>()
  const nodeKeyword = ref('')
  const detailTab = ref('overview')
  const error = ref('')
  const router = useRouter()
  const menuPages = ref<MenuPageOption[]>([])
  const pluginPurposes: Record<string, string> = {
    'official.ai': '配置 AI 模型，为工作流与助手提供模型调用。',
    'official.connector': '通过受控 HTTP、Webhook 和 WebSocket 连接外部服务。',
    'official.notification': '组合通知内容并发送站内、钉钉或邮件通知。',
    'official.qq': '通过 QQ 接收消息与发送通知。',
    'official.quant': '配置策略、计算指标并分析回测结果。',
    'official.binance': '查询币安品种与行情，查看模拟交易结果。'
  }
  const mountedPage = (page: InstalledPlugin['pages'][number]) =>
    menuPages.value.find(
      (item) =>
        item.pluginId === selectedPlugin.value?.id &&
        item.pageKey === page.pageKey &&
        item.contributionType === (page.kind === 'resultPage' ? 'result' : 'page')
    )?.menuId
  const openPage = (page: InstalledPlugin['pages'][number]) =>
    router.push(
      '/plugins/' +
        selectedPlugin.value!.id +
        '/' +
        (page.kind === 'resultPage' ? 'result' : 'page') +
        '/' +
        page.pageKey
    )

  const initialFilters = {
    keyword: '',
    contribution: ''
  }
  const {
    form: formFilters,
    applied: appliedFilters,
    apply,
    reset,
    queryError
  } = useRouteFilters(initialFilters)

  const defaultPluginVisual = {
    icon: 'ri:puzzle-2-line',
    color: '#475569',
    background: 'rgb(71 85 105 / 0.1)'
  }

  const contributionLabels: Record<string, string> = {
    nodes: '工作流节点',
    triggers: '流程触发',
    strategies: '量化策略',
    marketDataProviders: '行情服务',
    executionProviders: '交易执行',
    apiRoutes: '接口服务',
    pages: '管理页面',
    resultPages: '结果页面',
    assistantQueries: '助手查询',
    workflowValidators: '工作流校验',
    templates: '工作流模板',
    migrations: '数据升级',
    headerActions: '顶栏入口',
    runPanels: '运行面板'
  }

  const contributionIcons: Record<string, string> = {
    nodes: 'ri:node-tree',
    triggers: 'ri:flashlight-line',
    strategies: 'ri:stock-line',
    marketDataProviders: 'ri:line-chart-line',
    executionProviders: 'ri:exchange-line',
    apiRoutes: 'ri:route-line',
    pages: 'ri:layout-grid-line',
    resultPages: 'ri:file-chart-line',
    assistantQueries: 'ri:question-answer-line',
    workflowValidators: 'ri:shield-check-line',
    templates: 'ri:file-copy-2-line',
    migrations: 'ri:database-2-line',
    headerActions: 'ri:chat-3-line',
    runPanels: 'ri:layout-line'
  }

  const schemaTypeLabels: Record<string, string> = {
    string: '文本',
    integer: '整数',
    number: '数值',
    boolean: '布尔值',
    array: '列表',
    object: '对象'
  }

  const pluginVisual = (plugin: InstalledPlugin) => ({
    ...defaultPluginVisual,
    icon: plugin.menuIcon || defaultPluginVisual.icon
  })
  const pluginStyle = (plugin: InstalledPlugin) => ({
    '--plugin-color': pluginVisual(plugin).color,
    '--plugin-background': pluginVisual(plugin).background
  })
  const displayText = (value: unknown, fallback = '') => String(value ?? '').trim() || fallback
  const pluginLabel = (plugin: InstalledPlugin) => plugin.name || plugin.id
  const contributionLabel = (value: string) => contributionLabels[value] || '其他能力'
  const contributionIcon = (value: string) => contributionIcons[value] || 'ri:add-circle-line'
  const nodeKindLabel = (kind: 'action' | 'trigger') => (kind === 'trigger' ? '触发器' : '动作')
  const pageKindLabel = (kind: 'page' | 'resultPage') =>
    kind === 'resultPage' ? '结果页面' : '管理页面'

  const contributionOptions = computed(() => {
    const values = new Set(plugins.value.flatMap((plugin) => plugin.contributes))
    return Array.from(values)
      .map((value) => ({ value, label: contributionLabel(value) }))
      .sort((left, right) => left.label.localeCompare(right.label, 'zh-CN'))
  })

  const formItems = computed(() => [
    {
      label: '插件名称',
      key: 'keyword',
      type: 'input',
      props: { clearable: true, placeholder: '请输入插件名称' }
    },
    {
      label: '能力类型',
      key: 'contribution',
      type: 'select',
      props: {
        clearable: true,
        placeholder: '请选择能力类型',
        options: contributionOptions.value
      }
    }
  ])

  const filteredPlugins = computed(() => {
    const keyword = appliedFilters.value.keyword.trim()
    return plugins.value.filter((plugin) => {
      const matchesKeyword = !keyword || pluginLabel(plugin).includes(keyword)
      const matchesContribution =
        !appliedFilters.value.contribution ||
        plugin.contributes.includes(appliedFilters.value.contribution)
      return matchesKeyword && matchesContribution
    })
  })
  const filteredNodeCount = computed(() =>
    filteredPlugins.value.reduce((total, plugin) => total + plugin.nodes.length, 0)
  )
  const filteredPageCount = computed(() =>
    filteredPlugins.value.reduce((total, plugin) => total + plugin.pages.length, 0)
  )

  const configFields = (node: PluginNode) =>
    Object.entries(node.configSchema?.properties || {}) as Array<[string, ConfigSchema]>
  const selectedPlugin = computed(
    () =>
      filteredPlugins.value.find((plugin) => plugin.id === selectedPluginId.value) ||
      filteredPlugins.value[0]
  )
  const filteredNodes = computed(() => {
    const keyword = nodeKeyword.value.trim().toLowerCase()
    return (selectedPlugin.value?.nodes || []).filter(
      (node) =>
        node.title.toLowerCase().includes(keyword) || displayText(node.title).includes(keyword)
    )
  })
  const selectedNode = computed(
    () =>
      filteredNodes.value.find((node) => node.type === selectedNodeType.value) ||
      filteredNodes.value[0]
  )

  watch(
    () => selectedPlugin.value?.id,
    () => {
      selectedPluginId.value = selectedPlugin.value?.id
      selectedNodeType.value = undefined
      nodeKeyword.value = ''
      detailTab.value = 'overview'
    }
  )
  const isConfigRequired = (node: PluginNode, fieldName: string) =>
    Array.isArray(node.configSchema?.required) && node.configSchema.required.includes(fieldName)
  const schemaTitle = (schema: ConfigSchema, fieldName: string) =>
    displayText(schema.title, fieldName)
  const schemaTypeLabel = (value: unknown) =>
    Array.isArray(value)
      ? value.map((item) => schemaTypeLabels[String(item)] || '任意').join('、')
      : schemaTypeLabels[String(value)] || '任意'
  const formatDisplayValue = (value: unknown): string =>
    typeof value === 'boolean'
      ? value
        ? '是'
        : '否'
      : typeof value === 'object'
        ? JSON.stringify(value)
        : String(value ?? '')
  const schemaDefault = (schema: ConfigSchema) =>
    Object.prototype.hasOwnProperty.call(schema, 'default')
      ? formatDisplayValue(schema.default)
      : ''
  const schemaOptions = (schema: ConfigSchema) => {
    if (!Array.isArray(schema.enum)) return ''
    const optionLabels = Array.isArray(schema.enumLabels) ? schema.enumLabels : []
    return schema.enum
      .map((value: unknown, index: number) =>
        formatDisplayValue(optionLabels[index] === undefined ? value : optionLabels[index])
      )
      .filter(Boolean)
      .join('、')
  }
  const handleSearch = (filters: Record<string, string>) => {
    Object.assign(formFilters, filters)
    void apply()
  }
  const handleReset = () => {
    void reset()
  }
  const loadPlugins = async () => {
    if (loading.value) return
    loading.value = true
    error.value = ''
    try {
      const [items, pages] = await Promise.all([fetchGetInstalledPlugins(), fetchMenuPages()])
      plugins.value = items
      menuPages.value = pages
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      loading.value = false
    }
  }

  onMounted(() => void loadPlugins())
</script>

<style scoped lang="scss">
  .plugin-page {
    min-height: 0;
    color: var(--art-gray-900);
    letter-spacing: 0;
  }

  .plugin-toolbar,
  .plugin-summary,
  .plugin-row,
  .plugin-detail__title,
  .plugin-capabilities,
  .plugin-capabilities > span,
  .detail-tab-label,
  .node-row,
  .node-config__title,
  .node-config__heading,
  .parameter-tags,
  .parameter-meta,
  .detail-row {
    display: flex;
    align-items: center;
  }

  .plugin-toolbar {
    flex-shrink: 0;
    justify-content: space-between;
    min-height: 54px;
    padding: 0 4px;
  }

  .plugin-summary {
    flex-wrap: wrap;
    gap: 9px;
    font-size: 13px;
    color: var(--art-gray-600);
  }

  .plugin-summary strong {
    font-size: 18px;
    color: var(--art-gray-900);
  }

  .plugin-summary i {
    width: 1px;
    height: 12px;
    background: var(--art-gray-300);
  }

  .plugin-workspace {
    display: grid;
    flex: 1;
    grid-template-columns: 208px minmax(0, 1fr);
    min-height: 380px;
    background: var(--default-box-color);
    border-block: 1px solid var(--art-card-border);
  }

  .plugin-list {
    min-height: 0;
    padding: 12px 8px;
    overflow-y: auto;
    border-right: 1px solid var(--art-gray-300);
  }

  .plugin-row,
  .node-row {
    gap: 12px;
    width: 100%;
    padding: 12px;
    font: inherit;
    color: inherit;
    text-align: left;
    cursor: pointer;
    background: transparent;
    border: 0;
    border-left: 3px solid transparent;
    border-radius: 4px;
  }

  .plugin-row + .plugin-row {
    margin-top: 4px;
  }

  .plugin-row:hover,
  .node-row:hover {
    background: var(--art-gray-100);
  }

  .plugin-row:focus-visible,
  .node-row:focus-visible,
  .node-config:focus-visible {
    outline: 2px solid var(--plugin-color);
    outline-offset: -2px;
  }

  .plugin-row.is-selected,
  .node-row.is-selected {
    color: var(--plugin-color);
    background: var(--plugin-background);
    border-left-color: var(--plugin-color);
  }

  .plugin-mark {
    display: grid;
    flex: 0 0 32px;
    place-items: center;
    width: 32px;
    height: 32px;
    font-size: 19px;
    color: var(--plugin-color);
  }

  .plugin-copy,
  .node-copy {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .plugin-copy strong,
  .node-copy strong {
    display: block;
    font-size: 13px;
    font-weight: 600;
    line-height: 1.6;
  }

  .plugin-copy small,
  .node-copy small {
    display: block;
    margin-top: 4px;
    font-size: 11px;
    line-height: 1.5;
    color: var(--art-gray-600);
  }

  .plugin-detail {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    padding: 0 20px;
  }

  .plugin-detail__header {
    padding: 18px 0 4px;
  }

  .plugin-detail__title {
    flex-wrap: wrap;
    gap: 10px;
  }

  .plugin-detail__title h2 {
    margin: 0;
    font-size: 17px;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .plugin-version,
  .node-version {
    font-size: 12px;
    color: var(--art-gray-500);
  }

  .plugin-capabilities {
    flex-wrap: wrap;
    gap: 8px 16px;
    margin-top: 12px;
    font-size: 12px;
    color: var(--art-gray-600);
  }

  .plugin-capabilities > span {
    gap: 5px;
  }

  .plugin-detail__tabs {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
    margin-top: 12px;
  }

  .plugin-detail__tabs :deep(.el-tabs__header) {
    flex-shrink: 0;
    margin-bottom: 0;
  }

  .plugin-detail__tabs :deep(.el-tabs__content) {
    flex: 1;
    min-height: 0;
  }

  .plugin-detail__tabs :deep(.el-tab-pane) {
    height: 100%;
    overflow-y: auto;
  }

  .detail-tab-label {
    gap: 7px;
  }

  .detail-tab-label small {
    min-width: 20px;
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    color: var(--art-gray-500);
    text-align: center;
  }

  .node-browser {
    display: grid;
    grid-template-columns: 208px minmax(0, 1fr);
    height: 100%;
    min-height: 0;
  }

  .node-navigation {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    padding: 16px 12px 16px 0;
    border-right: 1px solid var(--art-gray-300);
  }

  .node-search {
    flex-shrink: 0;
    margin-bottom: 12px;
  }

  .node-list {
    min-height: 0;
    overflow-y: auto;
  }

  .node-row {
    min-height: 68px;
    padding: 10px;
    border-radius: 0;
  }

  .node-row > :last-child {
    flex-shrink: 0;
  }

  .node-config {
    min-width: 0;
    min-height: 0;
    padding: 20px 0 20px 24px;
    overflow-y: auto;
    overflow-wrap: anywhere;
  }

  .node-config__header {
    padding-bottom: 20px;
  }

  .node-location {
    margin: 0 0 10px;
    font-size: 12px;
    color: var(--art-gray-500);
  }

  .node-config__title {
    flex-wrap: wrap;
    gap: 10px;
    margin-bottom: 6px;
  }

  .node-config__title h3 {
    margin: 0;
    font-size: 16px;
    line-height: 1.6;
  }

  .node-config__heading {
    justify-content: space-between;
    padding-bottom: 10px;
    border-bottom: 1px solid var(--art-gray-300);
  }

  .node-config__heading h4 {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
  }

  .node-config__heading small {
    font-size: 12px;
    color: var(--art-gray-500);
  }

  .parameter-list {
    margin: 0;
  }

  .parameter-row {
    display: grid;
    grid-template-columns: minmax(120px, 1fr) minmax(0, 2fr);
    gap: 16px;
    padding: 16px 0;
    font-size: 12px;
    line-height: 1.7;
    border-bottom: 1px solid var(--art-gray-200);
  }

  .parameter-row dt,
  .parameter-row dd {
    min-width: 0;
    margin: 0;
  }

  .parameter-row dt > strong {
    font-weight: 600;
  }

  .parameter-tags {
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 6px;
    color: var(--art-gray-500);
  }

  .parameter-description,
  .parameter-meta {
    margin: 0;
    color: var(--art-gray-600);
  }

  .parameter-meta {
    gap: 10px;
    align-items: flex-start;
  }

  .parameter-row dd > p + p {
    margin-top: 6px;
  }

  .parameter-meta > span:first-child {
    flex-shrink: 0;
    color: var(--art-gray-500);
  }

  .page-list {
    padding-top: 8px;
  }

  .detail-row {
    gap: 12px;
    min-height: 58px;
    padding: 12px 0;
    border-bottom: 1px solid var(--art-gray-200);
  }

  .detail-row strong {
    flex: 1;
    min-width: 0;
    font-size: 13px;
    overflow-wrap: anywhere;
  }

  .detail-row > :last-child {
    flex-shrink: 0;
  }

  @media (width <= 1200px) {
    .plugin-workspace {
      grid-template-rows: auto minmax(0, 1fr);
      grid-template-columns: minmax(0, 1fr);
      min-height: 480px;
    }

    .plugin-list {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
      gap: 4px;
      max-height: 160px;
      padding: 8px;
      border-right: 0;
      border-bottom: 1px solid var(--art-gray-300);
    }

    .plugin-row {
      padding: 8px;
    }

    .plugin-row + .plugin-row {
      margin-top: 0;
    }
  }

  @media (width <= 900px) {
    .plugin-page {
      height: auto;
    }

    .plugin-workspace {
      min-height: 0;
    }

    .plugin-detail {
      padding: 0 12px;
    }

    .plugin-detail__tabs :deep(.el-tab-pane) {
      height: auto;
      overflow: visible;
    }

    .node-browser {
      grid-template-columns: minmax(0, 1fr);
      height: auto;
    }

    .node-navigation {
      max-height: 220px;
      padding: 12px 0;
      border-right: 0;
      border-bottom: 1px solid var(--art-gray-300);
    }

    .node-config {
      padding: 20px 0;
      overflow: visible;
    }

    .parameter-row {
      grid-template-columns: minmax(0, 1fr);
      gap: 8px;
    }
  }
  .plugin-purpose {
    max-width: 70ch;
    line-height: 1.8;
  }
  .plugin-overview {
    display: grid;
    gap: 12px;
    margin: 20px 0;
  }
  .plugin-overview > div {
    display: grid;
    grid-template-columns: 110px 1fr;
    gap: 12px;
  }
  .plugin-overview dt {
    color: var(--el-text-color-secondary);
  }
  .plugin-overview dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
</style>
