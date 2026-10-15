<template>
  <main v-loading="loading" class="run-page workflow-execution-detail">
    <div class="workflow-execution-detail__layout">
      <ElAlert
        v-if="error || run?.errorMessage"
        type="error"
        :title="error || run?.errorMessage || ''"
        :closable="false"
      />
      <ElAlert
        v-if="tasks.length"
        type="warning"
        :closable="false"
        :title="'此运行有 ' + taskTotal + ' 项待审批'"
        ><ElButton link type="primary" @click="inspectorVisible = true">打开审批面板</ElButton
        ><ElButton
          v-if="taskTotal > tasks.length"
          link
          @click="router.push({ path: '/scheduler/tasks', query: { runId: run?.id } })"
          >查看全部待办</ElButton
        ></ElAlert
      >
      <div class="workflow-execution-detail__stage">
        <div
          class="workflow-execution-detail__body"
          :class="{ 'workflow-execution-detail__body--inspector-hidden': !inspectorVisible }"
        >
          <div class="workflow-execution-detail__main">
            <div class="workflow-execution-detail__canvas-pane">
              <header class="run-header workflow-execution-detail__toolbar">
                <ElButton
                  class="workflow-execution-detail__icon-btn"
                  :icon="ArrowLeft"
                  aria-label="返回运行记录"
                  @click="
                    router.push({
                      path: '/scheduler/execution',
                      query: { workflowId: run?.workflowId }
                    })
                  "
                />
                <div class="workflow-execution-detail__header-main">
                  <div class="workflow-execution-detail__header-copy"
                    ><h1 class="workflow-execution-detail__title">运行 #{{ run?.id }}</h1></div
                  >
                  <div class="workflow-execution-detail__header-status"
                    ><ElTag>{{ runStatusLabel(run?.status) }}</ElTag
                    ><ElTag type="info"
                      >{{ run?.entryPoint }} ·
                      {{
                        run?.diagnostic
                          ? '诊断重放'
                          : triggerLabels[run?.triggerType || ''] || run?.triggerType
                      }}</ElTag
                    ></div
                  >
                </div>
                <ElSpace wrap class="workflow-execution-detail__toolbar-actions">
                  <ElButton @click="load">刷新</ElButton>
                  <ElButton
                    v-if="run && ['queued', 'running', 'waiting', 'retrying'].includes(run.status)"
                    :loading="acting === 'cancel'"
                    :disabled="!!acting"
                    @click="action('cancel')"
                    >取消运行</ElButton
                  >
                  <ElButton
                    v-if="run && run.status === 'failed'"
                    :loading="acting === 'retry'"
                    :disabled="!!acting || run.errorCategory === 'unknown_result'"
                    @click="action('retry')"
                    >重试</ElButton
                  >
                  <ElButton
                    v-if="run && ['succeeded', 'failed', 'cancelled'].includes(run.status)"
                    :loading="acting === 'replay'"
                    :disabled="!!acting"
                    @click="action('replay')"
                    >诊断重放</ElButton
                  >
                  <ElButton
                    class="workflow-execution-detail__panel-btn"
                    @click="inspectorVisible = !inspectorVisible"
                    >{{ inspectorVisible ? '收起面板' : '展开面板' }}</ElButton
                  >
                </ElSpace>
              </header>
              <WorkflowGraphCanvas
                v-if="revision"
                class="workflow-execution-canvas"
                :graph="revision.graph"
                :definitions="definitions"
                :states="states"
                readonly
                @select="selectNode"
              />
            </div>
            <section class="workflow-execution-detail__live-logs" aria-label="实时运行日志">
              <div class="workflow-execution-detail__live-logs-header"
                ><div class="workflow-execution-detail__live-logs-title"
                  >实时运行日志
                  <span class="workflow-execution-detail__live-logs-count"
                    >{{ run?.logs.length || 0 }} 条</span
                  ></div
                ><ElTag>{{ runStatusLabel(run?.status) }}</ElTag></div
              >
              <div class="workflow-execution-detail__live-logs-body">
                <ElEmpty v-if="!run?.logs.length" description="等待节点日志" />
                <div
                  v-for="(line, index) in run?.logs || []"
                  :key="index"
                  class="workflow-execution-detail__live-log-row"
                >
                  <span class="workflow-execution-detail__live-log-time">{{ line.loggedAt }}</span
                  ><span class="workflow-execution-detail__live-log-level">{{ line.level }}</span
                  ><span class="workflow-execution-detail__live-log-content">{{
                    line.message
                  }}</span>
                </div>
              </div>
            </section>
          </div>
          <aside
            v-if="inspectorVisible"
            class="workflow-execution-detail__inspector"
            aria-label="运行检查面板"
          >
            <div class="workflow-execution-detail__inspector-header"
              ><strong class="workflow-execution-detail__inspector-title">{{
                selectedNode?.nodeInstanceId || '运行概览'
              }}</strong
              ><ElButton v-if="selectedNode" link @click="clearSelection">清除选中</ElButton></div
            >
            <ElScrollbar class="workflow-execution-detail__inspector-scroll"
              ><div class="workflow-execution-detail__inspector-body">
                <section v-if="tasks.length" aria-label="待审批">
                  <div
                    v-for="task in tasks"
                    :key="task.id"
                    class="workflow-execution-detail__log-card"
                  >
                    <p>{{ task.prompt }}</p
                    ><p>截止 {{ task.expiresAt }}</p>
                    <ElSpace
                      ><ElButton
                        type="primary"
                        :loading="deciding === task.id"
                        :disabled="Boolean(deciding)"
                        @click="decide(task.id, 'approve')"
                        >通过</ElButton
                      ><ElButton
                        type="danger"
                        :disabled="Boolean(deciding)"
                        @click="decide(task.id, 'reject')"
                        >拒绝</ElButton
                      ></ElSpace
                    >
                  </div>
                </section>
                <ElTabs v-model="tab">
                  <ElTabPane label="概览" name="graph">
                    <ElDescriptions :column="1" border size="small"
                      ><ElDescriptionsItem label="工作流">{{
                        run?.workflowName
                      }}</ElDescriptionsItem
                      ><ElDescriptionsItem label="版本"
                        >v{{ run?.revisionNumber }}</ElDescriptionsItem
                      ><ElDescriptionsItem label="入口">{{ run?.entryPoint }}</ElDescriptionsItem
                      ><ElDescriptionsItem label="状态">{{
                        runStatusLabel(run?.status)
                      }}</ElDescriptionsItem></ElDescriptions
                    >
                    <div class="workflow-execution-detail__json-block"
                      ><h3>本次输入</h3><pre>{{ JSON.stringify(run?.input, null, 2) }}</pre>
                    </div>
                    <div class="workflow-execution-detail__json-block"
                      ><h3>结果摘要</h3><pre>{{ JSON.stringify(run?.resultSummary, null, 2) }}</pre>
                    </div>
                  </ElTabPane>
                  <ElTabPane label="节点记录" name="nodes">
                    <ElTable :data="run?.runNodes || []" @row-click="selectAttempt"
                      ><ElTableColumn
                        prop="nodeInstanceId"
                        label="节点"
                        min-width="130" /><ElTableColumn label="状态" min-width="100"
                        ><template #default="{ row }">{{
                          runStatusLabel(row.status)
                        }}</template></ElTableColumn
                      ><ElTableColumn prop="attempt" label="尝试" width="65"
                    /></ElTable>
                  </ElTabPane>
                  <ElTabPane v-if="selectedNode && run" label="节点详情" name="node">
                    <p
                      >尝试 {{ selectedNode.attempt }} · {{ runStatusLabel(selectedNode.status) }} ·
                      循环 {{ selectedNode.loopIteration }} · {{ selectedNode.durationMs }} ms</p
                    >
                    <ElAlert
                      v-if="selectedNode.errorMessage || panelError"
                      type="error"
                      :title="selectedNode.errorMessage || panelError"
                      :closable="false"
                    />
                    <ElTabs
                      ><ElTabPane label="输入"
                        ><div class="workflow-execution-detail__json-block">
                          <pre>{{ JSON.stringify(selectedNode.inputSummary, null, 2) }}</pre>
                        </div></ElTabPane
                      ><ElTabPane label="输出"
                        ><ElDescriptions
                          v-if="selectedOutput.length"
                          :column="1"
                          border
                          size="small"
                          ><ElDescriptionsItem
                            v-for="field in selectedOutput"
                            :key="field.key"
                            :label="field.title"
                            >{{ field.value }}</ElDescriptionsItem
                          ></ElDescriptions
                        >
                        <details class="workflow-execution-detail__json-block"
                          ><summary>原始 JSON</summary>
                          <pre>{{ JSON.stringify(selectedNode.outputSummary, null, 2) }}</pre>
                        </details></ElTabPane
                      ><ElTabPane v-for="panel in panels" :key="panel.id" :label="panel.title"
                        ><component
                          :is="panel.component"
                          :result="{
                            run,
                            runNode: selectedNode,
                            outputSchema: selectedDefinition?.outputSchema
                          }" /></ElTabPane
                    ></ElTabs>
                  </ElTabPane>
                  <ElTabPane label="制品" name="artifacts"
                    ><ElTable :data="run?.artifacts || []"
                      ><ElTableColumn prop="nodeInstanceId" label="节点" /><ElTableColumn
                        label="下载"
                        ><template #default="{ row }"
                          ><ElButton
                            link
                            :loading="downloading === row.sha256"
                            :disabled="!!downloading"
                            @click="download(row)"
                            >下载制品</ElButton
                          ></template
                        ></ElTableColumn
                      ></ElTable
                    ></ElTabPane
                  >
                </ElTabs>
              </div></ElScrollbar
            >
          </aside>
        </div>
      </div>
    </div>
  </main>
</template>
<script setup lang="ts">
  import { ElMessageBox } from 'element-plus'
  import { ArrowLeft } from '@element-plus/icons-vue'
  import type { Component } from 'vue'
  import {
    fetchWorkflowRun,
    fetchWorkflowHumanTasks,
    decideWorkflowHumanTask,
    type WorkflowHumanTask,
    fetchWorkflowRevision,
    fetchWorkflowNodeDefinitions,
    applyWorkflowRunAction,
    downloadWorkflowArtifact,
    type WorkflowRunDetail,
    type WorkflowRevision,
    type WorkflowNodeDefinition,
    type WorkflowRunNode,
    type WorkflowArtifact
  } from '@/api/workflows'
  import { fetchPluginCatalog, type PluginCatalogItem } from '@/api/pluginCatalog'
  import { loadPluginComponent } from '@/plugins'
  import { runStatusLabel } from '@/components/workflow/status'
  import { schemaOutputFields } from '@/components/workflow/output-summary'
  import { useMediaQuery } from '@vueuse/core'
  import WorkflowGraphCanvas from '@/components/workflow/WorkflowGraphCanvas.vue'
  const route = useRoute(),
    router = useRouter()
  const run = ref<WorkflowRunDetail>(),
    revision = ref<WorkflowRevision>(),
    definitions = ref<WorkflowNodeDefinition[]>([]),
    catalog = ref<PluginCatalogItem[]>([])
  const tasks = ref<WorkflowHumanTask[]>([]),
    inspectorVisible = ref(!useMediaQuery('(max-width: 900px)').value)
  const taskTotal = ref(0),
    deciding = ref(0)
  const triggerLabels: Record<string, string> = {
    manual: '手动触发',
    schedule: '定时触发',
    event: '事件触发',
    stream: '流式触发',
    webhook: 'Webhook 触发',
    failure: '失败触发'
  }
  const selectedDefinition = computed(() =>
    definitions.value.find((item) => item.type === selectedNode.value?.nodeType)
  )
  const selectedOutput = computed(() =>
    schemaOutputFields(
      selectedDefinition.value?.outputSchema || {},
      selectedNode.value?.outputSummary || {}
    )
  )
  const loading = ref(false),
    acting = ref(''),
    downloading = ref(''),
    error = ref(''),
    tab = ref('graph'),
    selectedNode = ref<WorkflowRunNode>(),
    panelError = ref('')
  const panels = shallowRef<{ id: string; title: string; component: Component }[]>([])
  const states = computed(() =>
    Object.fromEntries(
      (run.value?.runNodes || []).map((node) => [node.nodeInstanceId, node.status])
    )
  )
  let timer: ReturnType<typeof setTimeout> | undefined,
    requestID = 0
  const load = async () => {
    clearTimeout(timer)
    const request = ++requestID
    loading.value = !run.value
    error.value = ''
    try {
      const id = Number(route.params.executionId)
      if (!Number.isSafeInteger(id) || id <= 0) throw new Error('运行编号无效')
      const current = await fetchWorkflowRun(id)
      if (request !== requestID) return
      if (revision.value?.id !== current.revisionId) {
        const [snapshot, nodes, plugins] = await Promise.all([
          fetchWorkflowRevision(current.workflowId, current.revisionId),
          fetchWorkflowNodeDefinitions(),
          fetchPluginCatalog()
        ])
        if (request !== requestID) return
        revision.value = snapshot
        definitions.value = nodes.items
        catalog.value = plugins.items
      }
      const pending = await fetchWorkflowHumanTasks({
        status: 'pending',
        runId: current.id,
        limit: 100
      })
      if (request !== requestID) return
      tasks.value = pending.records
      taskTotal.value = pending.total
      run.value = current
      if (selectedNode.value)
        selectedNode.value = current.runNodes.find((node) => node.id === selectedNode.value?.id)
      if (['queued', 'running', 'waiting', 'retrying'].includes(current.status))
        timer = setTimeout(load, 3000)
    } catch (cause: any) {
      if (request === requestID) error.value = cause.message
    } finally {
      if (request === requestID) loading.value = false
    }
  }
  const action = async (action: 'cancel' | 'retry' | 'replay') => {
    if (
      !run.value ||
      acting.value ||
      (action === 'retry' && run.value.errorCategory === 'unknown_result')
    )
      return
    acting.value = action
    error.value = ''
    try {
      const result = await applyWorkflowRunAction(run.value.id, action)
      if (result.id !== run.value.id) await router.push(`/scheduler/execution/${result.id}/detail`)
      else await load()
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      acting.value = ''
    }
  }
  let panelRequest = 0
  const selectAttempt = async (node: WorkflowRunNode) => {
    selectedNode.value = node
    inspectorVisible.value = true
    tab.value = 'node'
    panels.value = []
    panelError.value = ''
    const request = ++panelRequest
    try {
      const candidates = catalog.value.flatMap((plugin) =>
        plugin.runPanels
          .filter((panel) => panel.nodeTypes.includes(node.nodeType))
          .map((panel) => ({ plugin, panel }))
      )
      const loaded = await Promise.all(
        candidates.map(async ({ plugin, panel }) => ({
          id: `${plugin.id}/${panel.panelKey}`,
          title: panel.title,
          component: await loadPluginComponent(plugin.id, 'runPanels', panel.panelKey)
        }))
      )
      if (request === panelRequest) panels.value = loaded
    } catch (cause: any) {
      if (request === panelRequest) panelError.value = cause.message
    }
  }
  const clearSelection = () => {
    selectedNode.value = undefined
    tab.value = 'graph'
    panelRequest++
    panels.value = []
  }
  const selectNode = (id: string, kind: 'node' | 'edge') => {
    if (kind !== 'node') return
    const node = run.value?.runNodes.filter((node) => node.nodeInstanceId === id).at(-1)
    if (node) void selectAttempt(node)
  }
  const decide = async (id: number, action: 'approve' | 'reject') => {
    if (deciding.value) return
    try {
      await ElMessageBox.confirm(`${action === 'approve' ? '通过' : '拒绝'}此待办？`, '处理待办', {
        confirmButtonText: '确定',
        cancelButtonText: '取消'
      })
    } catch {
      return
    }
    deciding.value = id
    try {
      await decideWorkflowHumanTask(id, action)
      await load()
    } catch (cause: any) {
      await load()
      error.value = cause.message + '；待办已刷新，请确认当前状态'
    } finally {
      deciding.value = 0
    }
  }
  const download = async (artifact: WorkflowArtifact) => {
    if (downloading.value) return
    downloading.value = artifact.sha256
    try {
      const blob = await downloadWorkflowArtifact(artifact.downloadUrl)
      const url = URL.createObjectURL(blob)
      try {
        const link = document.createElement('a')
        link.href = url
        link.download = artifact.sha256
        link.click()
      } finally {
        URL.revokeObjectURL(url)
      }
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      downloading.value = ''
    }
  }
  watch(
    () => route.params.executionId,
    () => {
      run.value = undefined
      revision.value = undefined
      tasks.value = []
      taskTotal.value = 0
      clearSelection()
      void load()
    },
    { immediate: true }
  )
  onBeforeUnmount(() => {
    requestID++
    panelRequest++
    clearTimeout(timer)
  })
</script>
<style scoped lang="scss">
  .workflow-execution-detail {
    --workflow-page-bg: var(--default-bg-color);
    --workflow-overlay-bg: var(--workflow-panel-bg);
    --workflow-overlay-raised: var(--workflow-panel-raised);
    --workflow-overlay-soft: var(--workflow-panel-soft);
    --workflow-overlay-soft-2: var(--art-gray-200);
    --workflow-overlay-text: var(--workflow-panel-text);
    --workflow-overlay-regular: var(--art-gray-800);
    --workflow-overlay-muted: var(--workflow-panel-muted);
    --workflow-overlay-placeholder: var(--art-gray-500);
    --workflow-overlay-border: var(--workflow-panel-border);
    --workflow-overlay-border-soft: var(--workflow-panel-border);
    --workflow-overlay-border-subtle: var(--art-card-border);

    flex: 1 1 auto;
    width: 100%;
    min-width: 0;
    height: 100%;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    overscroll-behavior: contain;
    background: var(--workflow-page-bg);
  }

  .workflow-execution-detail__layout {
    box-sizing: border-box;
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    gap: 12px;
    width: 100%;
    min-width: 0;
    min-height: 100%;
    padding: 10px;
    overflow: visible;
  }

  .workflow-execution-detail__stage {
    display: flex;
    flex: 0 0 auto;
    flex-direction: column;
    min-width: 0;
    min-height: 100%;
    overflow: visible;
  }

  .workflow-execution-detail__toolbar,
  .workflow-execution-detail__inspector {
    background: var(--workflow-overlay-bg);
    border: 1px solid var(--workflow-overlay-border);
    box-shadow: 0 6px 18px rgb(31 35 48 / 0.08);
  }

  .workflow-execution-detail__toolbar {
    position: relative;
    flex: 0 0 auto;
    box-sizing: border-box;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: 10px;
    align-items: center;
    width: auto;
    padding: 8px 10px;
    border-radius: 8px;
  }

  .workflow-execution-detail__panel-btn {
    gap: 6px;
    height: 36px;
    padding: 0 12px;
    color: var(--workflow-overlay-text);
    background: var(--workflow-overlay-bg);
    border-color: transparent;
    border-radius: 7px;
    box-shadow: none;
  }

  .workflow-execution-detail__toolbar-actions {
    display: flex;
    gap: 4px;
    align-items: center;
  }

  .workflow-execution-detail__icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 34px;
    height: 34px;
    padding: 0;
    color: var(--workflow-overlay-text);
    background: transparent;
    backdrop-filter: none;
    border-color: transparent;
    border-radius: 7px;
    box-shadow: none;
  }

  .workflow-execution-detail__icon-btn:hover,
  .workflow-execution-detail__panel-btn:hover {
    color: var(--theme-color);
    background: var(--el-color-primary-light-9);
  }

  .workflow-execution-detail__header-main {
    display: flex;
    gap: 10px;
    align-items: center;
    justify-content: space-between;
    min-width: 0;
  }

  .workflow-execution-detail__header-copy {
    flex: 1 1 auto;
    min-width: 0;
  }

  .workflow-execution-detail__title {
    overflow: hidden;
    font-size: 16px;
    font-weight: 700;
    line-height: 22px;
    color: var(--workflow-overlay-text);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .workflow-execution-detail__header-status {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    align-items: center;
    justify-content: flex-end;
  }

  .workflow-execution-detail__summary {
    display: flex;
    flex-wrap: nowrap;
    gap: 6px;
    align-items: center;
    overflow: hidden;
  }

  .workflow-execution-detail__summary-item {
    display: flex;
    flex: 0 1 auto;
    flex-direction: row;
    gap: 5px;
    align-items: center;
    min-width: 0;
    padding: 3px 7px;
    background: var(--workflow-overlay-soft-2);
    border: 1px solid var(--workflow-overlay-border-soft);
    border-radius: 6px;
  }

  .workflow-execution-detail__summary-label {
    font-size: 10px;
    color: var(--workflow-overlay-muted);
    white-space: nowrap;
  }

  .workflow-execution-detail__summary-item strong {
    overflow: hidden;
    font-size: 11px;
    line-height: 15px;
    color: var(--workflow-overlay-text);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .workflow-execution-detail__body {
    display: grid;
    flex: 0 0 auto;
    grid-template-columns: minmax(0, 1fr) 332px;
    gap: 10px;
    align-items: start;
    min-width: 0;
    min-height: 0;
    overflow: visible;
  }

  .workflow-execution-detail__body--inspector-hidden {
    grid-template-columns: minmax(0, 1fr);
  }

  .workflow-execution-detail__main {
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-width: 0;
    min-height: 0;
    overflow: visible;
  }

  .workflow-execution-detail__canvas-pane {
    position: relative;
    display: block;
    flex: 0 0 clamp(520px, 62vh, 720px);
    height: clamp(520px, 62vh, 720px);
    min-width: 0;
    min-height: 520px;
    overflow: hidden;
    isolation: isolate;
  }

  .workflow-execution-detail__canvas-pane > .workflow-execution-canvas {
    width: 100%;
    min-width: 0;
    height: 0;
    min-height: 0;
    flex: 1;
  }

  .workflow-execution-detail__live-logs {
    display: flex;
    flex: 0 0 clamp(320px, 38vh, 460px);
    flex-direction: column;
    height: clamp(320px, 38vh, 460px);
    min-height: 320px;
    overflow: hidden;
    background: var(--workflow-overlay-bg);
    border: 1px solid var(--workflow-overlay-border);
    border-radius: 8px;
    box-shadow: 0 8px 24px rgb(31 35 48 / 0.08);
  }

  .workflow-execution-detail__live-logs-header {
    display: flex;
    flex: 0 0 44px;
    align-items: center;
    justify-content: space-between;
    padding: 0 14px;
    border-bottom: 1px solid var(--workflow-overlay-border-subtle);
  }

  .workflow-execution-detail__live-logs-title {
    display: inline-flex;
    gap: 8px;
    align-items: center;
    font-size: 13px;
    font-weight: 700;
    color: var(--workflow-overlay-text);
  }

  .workflow-execution-detail__live-logs-actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .workflow-execution-detail__live-logs-dot {
    width: 7px;
    height: 7px;
    background: var(--workflow-overlay-muted);
    border-radius: 50%;
  }

  .workflow-execution-detail__live-logs-dot--active {
    background: var(--el-color-success);
    box-shadow: 0 0 0 3px var(--el-color-success-light-8);
    animation: workflow-live-log-pulse 1.8s ease-in-out infinite;
  }

  @keyframes workflow-live-log-pulse {
    0%,
    100% {
      opacity: 1;
    }

    50% {
      opacity: 0.45;
    }
  }

  .workflow-execution-detail__live-logs-count {
    font-size: 11px;
    font-weight: 500;
    color: var(--workflow-overlay-muted);
  }

  .workflow-execution-detail__live-logs-body {
    flex: 1 1 auto;
    min-height: 0;
    padding: 6px 0;
    overflow: auto;
    scrollbar-width: thin;
  }

  .workflow-execution-detail__live-log-row {
    display: grid;
    grid-template-columns: 142px 48px minmax(180px, 1fr);
    gap: 10px;
    align-items: start;
    min-height: 30px;
    padding: 6px 14px;
    font-family: 'Cascadia Code', SFMono-Regular, Consolas, monospace;
    font-size: 12px;
    border-bottom: 1px solid var(--workflow-overlay-border-subtle);
  }

  .workflow-execution-detail__live-log-row:last-child {
    border-bottom: 0;
  }

  .workflow-execution-detail__live-log-row time,
  .workflow-execution-detail__live-log-node {
    overflow: hidden;
    color: var(--workflow-overlay-muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .workflow-execution-detail__live-log-level {
    font-size: 10px;
    font-weight: 700;
    color: var(--el-color-primary);
  }

  .workflow-execution-detail__live-log-level[data-level='warn'] {
    color: var(--el-color-warning);
  }

  .workflow-execution-detail__live-log-level[data-level='error'] {
    color: var(--el-color-danger);
  }

  .workflow-execution-detail__live-log-message {
    display: block;
    color: var(--workflow-overlay-text);
    word-break: break-word;
  }

  .workflow-execution-detail__live-log-content {
    display: flex;
    min-width: 0;
    flex-direction: column;
    gap: 6px;
  }

  .workflow-execution-detail__live-log-fields {
    max-height: 180px;
    margin: 0;
    padding: 6px 8px;
    overflow: auto;
    font-size: 11px;
    line-height: 16px;
    color: var(--workflow-overlay-muted);
    word-break: break-word;
    white-space: pre-wrap;
    background: var(--workflow-overlay-soft);
    border-radius: 4px;
  }

  .workflow-execution-detail__inspector {
    --el-bg-color: var(--workflow-overlay-bg);
    --el-fill-color-blank: var(--workflow-overlay-raised);
    --el-fill-color-light: var(--workflow-overlay-soft);
    --el-fill-color-lighter: var(--workflow-overlay-soft-2);
    --el-border-color-light: var(--workflow-overlay-border-soft);
    --el-border-color-lighter: var(--workflow-overlay-border-subtle);
    --el-text-color-primary: var(--workflow-overlay-text);
    --el-text-color-regular: var(--workflow-overlay-regular);
    --el-text-color-secondary: var(--workflow-overlay-muted);
    --el-text-color-placeholder: var(--workflow-overlay-placeholder);

    position: sticky;
    top: 0;
    display: flex;
    flex-direction: column;
    width: 100%;
    height: clamp(520px, calc(100vh - 160px), 720px);
    min-width: 0;
    min-height: 520px;
    overflow: hidden;
    isolation: isolate;
    border-radius: 8px;
  }

  .workflow-execution-detail__inspector-header {
    display: flex;
    gap: 12px;
    align-items: center;
    justify-content: space-between;
    padding: 14px 16px;
    border-bottom: 1px solid var(--workflow-overlay-border-subtle);
  }

  .workflow-execution-detail__inspector-title {
    min-width: 0;
    overflow: hidden;
    font-size: 15px;
    font-weight: 700;
    color: var(--workflow-overlay-text);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .workflow-execution-detail__inspector-scroll {
    flex: 1 1 auto;
    min-height: 0;
  }

  .workflow-execution-detail__inspector-scroll :deep(.el-scrollbar__wrap) {
    height: 100%;
    overflow-x: hidden;
  }

  .workflow-execution-detail__inspector-body {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 14px;
  }

  .workflow-execution-detail__section {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .workflow-execution-detail__section-title {
    font-size: 13px;
    font-weight: 700;
    color: var(--workflow-overlay-regular);
  }

  .workflow-execution-detail__log-card {
    padding: 14px;
    background: var(--workflow-overlay-raised);
    border: 1px solid var(--workflow-overlay-border-subtle);
    border-radius: 8px;
    box-shadow: none;
  }

  .workflow-execution-detail__json-group {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .workflow-execution-detail__json-block {
    padding: 12px;
    background: var(--workflow-overlay-soft);
    border: 1px solid var(--workflow-overlay-border-soft);
    border-radius: 7px;
  }

  .workflow-execution-detail__json-title {
    margin-bottom: 10px;
    font-size: 12px;
    font-weight: 700;
    color: var(--workflow-overlay-muted);
  }

  .workflow-execution-detail__json-block pre {
    margin: 0;
    font-family: 'Cascadia Code', SFMono-Regular, Consolas, monospace;
    font-size: 12px;
    line-height: 18px;
    color: var(--workflow-overlay-text);
    word-break: break-word;
    white-space: pre-wrap;
  }

  .workflow-execution-detail__summary-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }

  .workflow-execution-detail__summary-item {
    display: grid;
    gap: 4px;
    min-width: 0;
    padding: 8px 10px;
    background: var(--workflow-overlay-raised);
    border: 1px solid var(--workflow-overlay-border-subtle);
    border-radius: 6px;
  }

  .workflow-execution-detail__summary-item span {
    overflow: hidden;
    font-size: 10px;
    color: var(--workflow-overlay-muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .workflow-execution-detail__summary-item strong {
    overflow: hidden;
    font-size: 12px;
    color: var(--workflow-overlay-text);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .workflow-execution-detail__summary-empty {
    display: block;
    padding: 4px 0;
    font-size: 12px;
    color: var(--workflow-overlay-muted);
  }

  .workflow-execution-detail__raw {
    margin-top: 10px;
    font-size: 11px;
    color: var(--workflow-overlay-muted);
  }

  .workflow-execution-detail__raw summary {
    width: fit-content;
    cursor: pointer;
    user-select: none;
  }

  .workflow-execution-detail__raw pre {
    margin-top: 8px;
  }

  @media (max-width: 460px) {
    .workflow-execution-detail__summary-grid {
      grid-template-columns: 1fr;
    }
  }

  .workflow-execution-detail__alert {
    margin-top: -4px;
  }

  @media (max-width: 1280px) {
    .workflow-execution-detail__body {
      grid-template-columns: minmax(0, 1fr) 304px;
    }

    .workflow-execution-detail__body--inspector-hidden {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  @media (max-width: 980px) {
    .workflow-execution-detail__body,
    .workflow-execution-detail__body--inspector-hidden {
      display: flex;
      flex-direction: column;
      overflow: visible;
    }

    .workflow-execution-detail__main {
      flex: 0 0 auto;
      min-height: 0;
      overflow: visible;
    }

    .workflow-execution-detail__canvas-pane {
      flex-basis: 480px;
      height: 480px;
      min-height: 480px;
    }

    .workflow-execution-detail__live-logs {
      flex-basis: 360px;
      height: 360px;
      min-height: 360px;
    }

    .workflow-execution-detail__inspector {
      position: static;
      flex: 0 0 520px;
      height: 520px;
      min-height: 360px;
    }
  }

  @media (max-width: 768px) {
    .workflow-execution-detail__layout {
      padding: 8px;
    }

    .workflow-execution-detail__toolbar {
      gap: 8px;
    }

    .workflow-execution-detail__canvas-pane {
      flex-basis: 400px;
      height: 400px;
      min-height: 400px;
    }

    .workflow-execution-detail__live-log-row {
      grid-template-columns: 112px 44px minmax(0, 1fr);
      gap: 8px;
    }

    .workflow-execution-detail__live-log-content {
      grid-column: 3;
    }

    .workflow-execution-detail__header-status {
      display: none;
    }

    .workflow-execution-detail__live-logs-header {
      gap: 8px;
      min-height: 44px;
      padding: 6px 10px;
    }
  }

  h1 {
    margin: 0;
  }
  .workflow-execution-detail__canvas-pane {
    position: relative;
    display: flex;
    flex-direction: column;
  }
  .workflow-execution-canvas {
    flex: 1;
    min-height: 0;
    height: 0;
  }
</style>
