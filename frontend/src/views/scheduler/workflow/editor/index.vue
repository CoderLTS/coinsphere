<template>
  <main
    v-loading="loading || saving || publishing || revisionLoading"
    class="workflow-page workflow-editor-page"
  >
    <WorkflowEditorToolbar
      class="workflow-editor-page__toolbar"
      :mode="workflow ? 'edit' : 'create'"
      :name="metadata.name"
      :version-text="versionText"
      :saving="saving"
      :validating="validating"
      :read-only="editingDisabled"
      :can-undo="!!history.length"
      :can-redo="!!future.length"
      :status-text="!canEdit ? '历史版本 · 只读' : dirty ? '未保存' : '已保存'"
      :status-type="dirty ? 'warning' : 'default'"
      :zoom-text="zoomText"
      :materials-visible="materialsVisible"
      :json-visible="jsonVisible"
      @back="router.push('/scheduler/definition')"
      @save="save"
      @validate="validate"
      @undo="undo"
      @redo="redo"
      @fit-view="graphEditor?.fit()"
      @center-content="graphEditor?.center()"
      @zoom-in="graphEditor?.zoom(0.1)"
      @zoom-out="graphEditor?.zoom(-0.1)"
      @toggle-materials="materialsVisible = !materialsVisible"
      @toggle-json="jsonVisible = !jsonVisible"
      @open-meta="metaVisible = !metaVisible"
    >
      <template #actions>
        <ElButton v-if="workflow" @click="openRun">试运行</ElButton>
        <ElButton
          v-if="workflow"
          :disabled="dirty || !canEdit"
          :loading="publishing"
          @click="publish"
          >发布草稿</ElButton
        >
        <ElButton
          v-if="workflow"
          :disabled="!workflow.publishedRevisionId"
          :loading="changingStatus"
          @click="toggleEnabled"
          >{{ workflow.status === 'active' ? '停用' : '启用' }}</ElButton
        >
      </template>
    </WorkflowEditorToolbar>
    <ElAlert v-if="error" class="editor-alert" :title="error" type="error" :closable="false" />
    <WorkflowMetaPopover
      class="workflow-editor-page__meta workflow-meta"
      :visible="metaVisible"
      :model="metadata"
      :read-only="editingDisabled"
      @update:model="Object.assign(metadata, $event)"
      @submit="applyMetadata"
      @close="metaVisible = false"
    >
      <p v-if="workflow">{{ versionText }}</p>
      <ElFormItem v-if="revisions.length" label="查看版本">
        <ElSelect
          v-model="selectedRevisionID"
          :disabled="saving || publishing || revisionLoading"
          @change="loadRevision"
        >
          <ElOption
            v-for="item in revisions"
            :key="item.id"
            :value="item.id"
            :label="`v${item.revisionNumber}${item.id === workflow?.draftRevisionId ? ' · 草稿' : ''}${item.id === workflow?.publishedRevisionId ? ' · 已发布' : ''}`"
          />
        </ElSelect>
      </ElFormItem>
    </WorkflowMetaPopover>
    <WorkflowGraphEditor
      ref="graphEditor"
      class="workflow-editor-page__canvas"
      v-model:materials-visible="materialsVisible"
      @edit-start="beginFieldEdit"
      @edit-end="endFieldEdit"
      @zoom="zoomText = `${Math.round($event * 100)}%`"
      :graph="graph"
      :definitions="definitions"
      :read-only="editingDisabled"
      :secret-fields="revision?.secretFields"
      :secret-changes="secretChanges"
      :can-manage-secrets="canManageSecrets"
      @update:graph="commit"
      @update:secret-changes="secretChanges = $event"
    />
    <details v-if="issues.length" class="validation-panel" open>
      <summary>校验结果 · {{ issues.length }} 项问题</summary>
      <button v-for="(issue, index) in issues" :key="index" @click="graphEditor?.focusIssue(issue)"
        >{{ issue.message
        }}<span v-if="issue.nodeInstanceId || issue.edgeId"> · 点击定位</span></button
      >
    </details>
    <ElDrawer v-model="jsonVisible" title="完整定义" size="min(600px, 100vw)">
      <ElInput
        v-model="jsonDraft"
        :disabled="editingDisabled"
        type="textarea"
        :rows="24"
        aria-label="完整工作流定义"
      />
      <ElButton :disabled="editingDisabled" @click="applyJSON">应用定义</ElButton>
    </ElDrawer>
    <ElDialog v-model="runVisible" title="试运行工作流" width="min(640px, 94vw)"
      ><ElAlert
        title="试运行会实际执行所选版本的节点，包括外部请求和通知。请确认配置与输入后执行。"
        type="warning"
        :closable="false"
      />
      <ElAlert v-if="error" :title="error" type="error" :closable="false" />
      <ElForm label-position="top">
        <ElFormItem label="执行版本"
          ><ElSelect v-model="runRevisionID" aria-label="执行版本" @change="loadRunRevision"
            ><ElOption
              v-for="item in revisions"
              :key="item.id"
              :value="item.id"
              :label="
                'v' +
                item.revisionNumber +
                (item.id === workflow?.draftRevisionId ? ' · 草稿' : '') +
                (item.id === workflow?.publishedRevisionId ? ' · 已发布' : '')
              " /></ElSelect
        ></ElFormItem>
        <ElFormItem label="入口"
          ><ElSelect v-model="runEntry"
            ><ElOption
              v-for="(_, name) in runGraph?.entryPoints || {}"
              :key="name"
              :label="String(name)"
              :value="name" /></ElSelect></ElFormItem
        ><WorkflowSchemaFields
          :schema="runSchema"
          :config="runInput"
          @update="(key, value) => (runInput[key] = value)" /></ElForm
      ><details
        ><summary>完整输入</summary><ElInput v-model="runJSON" type="textarea" :rows="6" /></details
      ><template #footer
        ><ElButton @click="runVisible = false">取消</ElButton
        ><ElButton
          type="primary"
          :loading="running"
          :disabled="!runGraph || runVersionLoading"
          @click="run"
          >加入运行队列</ElButton
        ></template
      ></ElDialog
    >
  </main>
</template>
<script setup lang="ts">
  import { ElMessage, ElMessageBox } from 'element-plus'
  import { onBeforeRouteLeave } from 'vue-router'
  import {
    createWorkflow,
    fetchWorkflow,
    fetchWorkflowRevision,
    fetchWorkflowRevisions,
    fetchWorkflowNodeDefinitions,
    saveWorkflowRevision,
    publishWorkflow,
    validateWorkflowGraph,
    createWorkflowRun,
    applyWorkflowLifecycle,
    type WorkflowValidationIssue,
    type WorkflowGraph,
    type WorkflowDetail,
    type WorkflowRevision,
    type WorkflowNodeDefinition,
    type WorkflowSecretChange
  } from '@/api/workflows'
  import WorkflowEditorToolbar from '@/components/workflow/WorkflowEditorToolbar.vue'
  import WorkflowMetaPopover from '@/components/workflow/WorkflowMetaPopover.vue'
  import WorkflowGraphEditor from '@/components/workflow/WorkflowGraphEditor.vue'
  import WorkflowSchemaFields from '@/components/workflow/WorkflowSchemaFields.vue'
  import { cloneGraph } from '@/components/workflow/graph'
  const router = useRouter(),
    route = useRoute()
  const applyMetadata = (value: { name: string; description: string }) => {
    Object.assign(metadata, value)
    metaVisible.value = false
  }
  const graphEditor = ref<InstanceType<typeof WorkflowGraphEditor>>()
  const materialsVisible = ref(
      typeof window === 'undefined' || !window.matchMedia('(max-width: 900px)').matches
    ),
    jsonVisible = ref(false),
    metaVisible = ref(false),
    zoomText = ref('100%')
  const workflow = ref<WorkflowDetail>(),
    revision = ref<WorkflowRevision>(),
    revisions = ref<WorkflowRevision[]>([]),
    definitions = ref<WorkflowNodeDefinition[]>([])
  const graph = ref<WorkflowGraph>({
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
  })
  const metadata = reactive({ name: '', description: '' }),
    secretChanges = ref<WorkflowSecretChange[]>([]),
    selectedRevisionID = ref(0),
    clean = ref('')
  const loading = ref(false),
    revisionLoading = ref(false),
    saving = ref(false),
    publishing = ref(false),
    validating = ref(false),
    running = ref(false),
    error = ref(''),
    jsonDraft = ref('')
  const history = ref<WorkflowGraph[]>([]),
    future = ref<WorkflowGraph[]>([])
  const snapshot = () => JSON.stringify({ graph: graph.value, metadata })
  const dirty = computed(() => snapshot() !== clean.value || secretChanges.value.length > 0)
  watch(
    graph,
    () => {
      jsonDraft.value = JSON.stringify(graph.value, null, 2)
    },
    { deep: true, immediate: true }
  )
  const canEdit = computed(
    () => !workflow.value || selectedRevisionID.value === workflow.value.draftRevisionId
  )
  const editingDisabled = computed(
    () => !canEdit.value || saving.value || publishing.value || revisionLoading.value
  )
  const issues = ref<WorkflowValidationIssue[]>([])
  const versionText = computed(() =>
    workflow.value
      ? '草稿 v' +
        (revisions.value.find((item) => item.id === workflow.value?.draftRevisionId)
          ?.revisionNumber || '—') +
        ' / 已发布 ' +
        (revisions.value.find((item) => item.id === workflow.value?.publishedRevisionId)
          ?.revisionNumber
          ? 'v' +
            revisions.value.find((item) => item.id === workflow.value?.publishedRevisionId)
              ?.revisionNumber
          : '暂无')
      : '新建工作流'
  )
  let fieldEditing = false,
    fieldRecorded = false
  const beginFieldEdit = () => {
    if (!fieldEditing) {
      fieldEditing = true
      fieldRecorded = false
    }
  }
  const endFieldEdit = () => {
    fieldEditing = false
    fieldRecorded = false
  }
  const canManageSecrets = computed(() => true)
  const commit = (next: WorkflowGraph) => {
    if (editingDisabled.value || JSON.stringify(next) === JSON.stringify(graph.value)) return
    if (!fieldEditing || !fieldRecorded) history.value.push(cloneGraph(graph.value))
    fieldRecorded = fieldEditing
    issues.value = []
    if (history.value.length > 100) history.value.shift()
    future.value = []
    graph.value = next
  }
  const undo = () => {
    endFieldEdit()
    if (editingDisabled.value) return
    const previous = history.value.pop()
    if (previous) {
      future.value.push(cloneGraph(graph.value))
      graph.value = previous
    }
  }
  const redo = () => {
    endFieldEdit()
    if (editingDisabled.value) return
    const next = future.value.pop()
    if (next) {
      history.value.push(cloneGraph(graph.value))
      graph.value = next
    }
  }
  const applyJSON = () => {
    try {
      const next = JSON.parse(jsonDraft.value)
      if (
        next.schemaVersion !== 3 ||
        !Array.isArray(next.nodes) ||
        !Array.isArray(next.edges) ||
        !next.entryPoints
      )
        throw new Error('定义需要节点、连线与入口')
      commit(next)
      error.value = ''
    } catch (cause: any) {
      error.value = cause.message
    }
  }
  const loadRevision = async (id: number) => {
    if (!workflow.value) return
    if (revisionLoading.value) {
      selectedRevisionID.value = revision.value?.id || 0
      return
    }
    revisionLoading.value = true
    try {
      if (dirty.value) {
        try {
          await ElMessageBox.confirm('切换版本将放弃未保存的改动', '切换版本')
        } catch {
          selectedRevisionID.value = revision.value?.id || 0
          return
        }
      }
      const loaded = await fetchWorkflowRevision(workflow.value.id, id)
      endFieldEdit()
      Object.assign(metadata, {
        name: workflow.value.name,
        description: workflow.value.description
      })
      revision.value = loaded
      selectedRevisionID.value = loaded.id
      graph.value = cloneGraph(loaded.graph)
      secretChanges.value = []
      history.value = []
      future.value = []
      issues.value = []
      clean.value = snapshot()
      error.value = ''
    } catch (cause: any) {
      selectedRevisionID.value = revision.value?.id || 0
      error.value = cause.message
    } finally {
      revisionLoading.value = false
    }
  }
  const refresh = async (id: number) => {
    const [item, list] = await Promise.all([fetchWorkflow(id), fetchWorkflowRevisions(id)])
    workflow.value = item
    revisions.value = list.items
    Object.assign(metadata, { name: item.name, description: item.description })
    selectedRevisionID.value = item.draftRevisionId
    clean.value = snapshot()
    await loadRevision(item.draftRevisionId)
  }
  const validate = async () => {
    validating.value = true
    error.value = ''
    try {
      const result = await validateWorkflowGraph(graph.value)
      issues.value = result.issues
      if (!result.valid) error.value = '定义存在问题，请在底部校验结果中点击定位'
      else ElMessage.success('定义检查通过')
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      validating.value = false
    }
  }
  const save = async () => {
    if (editingDisabled.value) return
    saving.value = true
    error.value = ''
    try {
      if (!workflow.value) {
        const item = await createWorkflow({
          ...metadata,
          templateKey: 'blank',
          graph: graph.value,
          secretChanges: secretChanges.value
        })
        clean.value = snapshot()
        secretChanges.value = []
        await refresh(item.id)
        await router.replace(`/scheduler/workflow/${item.id}/edit`)
      } else {
        const saved = await saveWorkflowRevision(workflow.value.id, {
          expectedDraftRevisionId: workflow.value.draftRevisionId,
          metadata,
          graph: graph.value,
          secretChanges: secretChanges.value
        })
        revision.value = saved
        workflow.value.draftRevisionId = saved.id
        selectedRevisionID.value = saved.id
        secretChanges.value = []
        workflow.value.name = metadata.name
        workflow.value.description = metadata.description
        revisions.value = [saved, ...revisions.value]
        endFieldEdit()
        clean.value = snapshot()
      }
      ElMessage.success('草稿已保存')
    } catch (cause: any) {
      error.value = cause.message + '；当前编辑内容已保留，请核对草稿版本后重试'
    } finally {
      saving.value = false
    }
  }
  const publish = async () => {
    if (
      !workflow.value ||
      saving.value ||
      publishing.value ||
      revisionLoading.value ||
      selectedRevisionID.value !== workflow.value.draftRevisionId ||
      dirty.value
    )
      return
    publishing.value = true
    try {
      workflow.value = await publishWorkflow(
        workflow.value.id,
        workflow.value.draftRevisionId,
        workflow.value.publishedRevisionId
      )
      ElMessage.success('草稿已发布')
    } catch (cause: any) {
      error.value = cause.message + '；当前编辑内容已保留，请刷新版本信息后处理冲突'
    } finally {
      publishing.value = false
    }
  }
  const changingStatus = ref(false)
  const toggleEnabled = async () => {
    if (!workflow.value || changingStatus.value || saving.value || publishing.value) return
    changingStatus.value = true
    try {
      workflow.value = await applyWorkflowLifecycle(
        workflow.value.id,
        workflow.value.status === 'active' ? 'deactivate' : 'activate'
      )
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      changingStatus.value = false
    }
  }
  const runRevisionID = ref(0),
    runVersionLoading = ref(false)
  const runVisible = ref(false),
    runEntry = ref('main'),
    runInput = reactive<Record<string, unknown>>({}),
    runJSON = ref('{}'),
    runGraph = ref<WorkflowGraph>()
  const runSchema = computed(
    () =>
      definitions.value.find(
        (item) =>
          item.type ===
          runGraph.value?.nodes.find(
            (node) => node.nodeInstanceId === runGraph.value?.entryPoints[runEntry.value]
          )?.nodeType
      )?.inputSchema || {}
  )
  watch(
    runInput,
    () => {
      runJSON.value = JSON.stringify(runInput, null, 2)
    },
    { deep: true }
  )
  let runVersionRequest = 0
  const loadRunRevision = async () => {
    if (!workflow.value) return
    const current = ++runVersionRequest
    runVersionLoading.value = true
    runGraph.value = undefined
    error.value = ''
    try {
      const snapshot = await fetchWorkflowRevision(workflow.value.id, runRevisionID.value)
      if (current !== runVersionRequest) return
      runGraph.value = snapshot.graph
      runEntry.value = Object.keys(snapshot.graph.entryPoints)[0] || 'main'
      Object.keys(runInput).forEach((key) => delete runInput[key])
      runJSON.value = '{}'
    } catch (cause: any) {
      if (current === runVersionRequest) error.value = cause.message
    } finally {
      if (current === runVersionRequest) runVersionLoading.value = false
    }
  }
  const openRun = async () => {
    if (!workflow.value) return
    if (dirty.value) {
      ElMessage.warning('请先明确点击“保存草稿”，再选择版本试运行')
      return
    }
    runRevisionID.value = selectedRevisionID.value
    runVisible.value = true
    await loadRunRevision()
  }
  const run = async () => {
    if (!workflow.value || !runGraph.value || runVersionLoading.value || running.value) return
    running.value = true
    try {
      const input = JSON.parse(runJSON.value)
      const current = await createWorkflowRun(workflow.value.id, {
        revisionId: runRevisionID.value,
        entryPoint: runEntry.value,
        input
      })
      runVisible.value = false
      await router.push(`/scheduler/execution/${current.id}/detail`)
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      running.value = false
    }
  }
  onBeforeRouteLeave(async () => {
    if (!dirty.value) return true
    try {
      await ElMessageBox.confirm('离开将放弃未保存的改动', '离开编辑器')
      return true
    } catch {
      return false
    }
  })
  onMounted(async () => {
    loading.value = true
    try {
      definitions.value = (await fetchWorkflowNodeDefinitions()).items
      const id = Number(route.params.definitionId)
      if (id) await refresh(id)
      else clean.value = snapshot()
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      loading.value = false
    }
  })
</script>
<style scoped lang="scss">
  .workflow-editor-page {
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
    --workflow-overlay-hover: var(--art-hover-color);

    position: relative;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    width: 100%;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    background: var(--workflow-page-bg);
    padding: 10px;
  }

  .workflow-editor-page__toolbar {
    position: relative;
    flex: 0 0 auto;
    margin-bottom: 10px;
  }

  .workflow-editor-page__canvas {
    flex: 1 1 auto;
    min-height: 0;
  }

  .workflow-editor-page__meta {
    position: absolute;
    top: 76px;
    left: 50%;
    z-index: 30;
    transform: translateX(-50%);
  }

  @media (max-width: 768px) {
    .workflow-editor-page {
      padding: 8px;
    }

    .workflow-editor-page__toolbar {
      position: relative;
    }

    .workflow-editor-page__meta {
      top: 108px;
      right: 8px;
      left: 8px;
      transform: none;
    }
  }

  .editor-alert {
    flex: 0 0 auto;
    margin-bottom: 10px;
  }
  .workflow-editor-page {
    height: calc(100vh - 130px);
    min-height: 400px;
  }
  .validation-panel {
    flex: 0 0 auto;
    max-height: 180px;
    overflow: auto;
    margin-top: 8px;
    padding: 8px 12px;
    background: var(--workflow-panel-bg);
    border: 1px solid var(--workflow-panel-border);
    border-radius: 6px;
  }
  .validation-panel summary {
    cursor: pointer;
    color: var(--el-color-danger);
  }
  .validation-panel button {
    display: block;
    width: 100%;
    padding: 6px 0;
    text-align: left;
    color: var(--el-text-color-primary);
    background: transparent;
    border: 0;
    cursor: pointer;
  }
  .validation-panel button:hover {
    color: var(--el-color-primary);
  }
</style>
