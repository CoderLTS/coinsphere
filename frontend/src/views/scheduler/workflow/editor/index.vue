<template>
  <main v-loading="loading" class="workflow-page workflow-editor-page">
    <WorkflowEditorToolbar
      class="workflow-editor-page__toolbar"
      :mode="workflow ? 'edit' : 'create'"
      :name="metadata.name"
      :saving="saving"
      :validating="validating"
      :read-only="!canEdit"
      :can-undo="!!history.length"
      :can-redo="!!future.length"
      :status-text="dirty ? '未保存' : '已保存'"
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
        <ElButton
          v-if="workflow"
          :disabled="dirty || selectedRevisionID !== workflow.draftRevisionId"
          :loading="publishing"
          @click="publish"
          >发布草稿</ElButton
        >
        <ElButton v-if="workflow" :disabled="!workflow.publishedRevisionId" @click="openRun"
          >运行</ElButton
        >
      </template>
    </WorkflowEditorToolbar>
    <ElAlert v-if="error" class="editor-alert" :title="error" type="error" :closable="false" />
    <WorkflowMetaPopover
      class="workflow-editor-page__meta workflow-meta"
      :visible="metaVisible"
      :model="metadata"
      :read-only="!canEdit"
      @update:model="Object.assign(metadata, $event)"
      @submit="applyMetadata"
      @close="metaVisible = false"
    >
      <p v-if="workflow"
        >草稿 #{{ workflow.draftRevisionId }} · 已发布
        {{ workflow.publishedRevisionId ? '#' + workflow.publishedRevisionId : '暂无' }}</p
      >
      <ElFormItem v-if="revisions.length" label="查看版本">
        <ElSelect v-model="selectedRevisionID" @change="loadRevision">
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
      :materials-visible="materialsVisible"
      @zoom="zoomText = `${Math.round($event * 100)}%`"
      :graph="graph"
      :definitions="definitions"
      :read-only="!canEdit"
      :secret-fields="revision?.secretFields"
      :secret-changes="secretChanges"
      :can-manage-secrets="canManageSecrets"
      @update:graph="commit"
      @update:secret-changes="secretChanges = $event"
    />
    <ElDrawer v-model="jsonVisible" title="完整定义" size="min(600px, 100vw)">
      <ElInput
        v-model="jsonDraft"
        :disabled="!canEdit"
        type="textarea"
        :rows="24"
        aria-label="完整工作流定义"
      />
      <ElButton :disabled="!canEdit" @click="applyJSON">应用定义</ElButton>
    </ElDrawer>
    <ElDialog v-model="runVisible" title="运行工作流" width="min(640px, 94vw)"
      ><ElForm label-position="top"
        ><ElFormItem label="入口"
          ><ElSelect v-model="runEntry"
            ><ElOption
              v-for="(_, name) in publishedGraph?.entryPoints || {}"
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
        ><ElButton type="primary" :loading="running" @click="run">加入运行队列</ElButton></template
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
  const materialsVisible = ref(true),
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
  const canEdit = computed(() => true)
  const canManageSecrets = computed(() => true)
  const commit = (next: WorkflowGraph) => {
    if (!canEdit.value || JSON.stringify(next) === JSON.stringify(graph.value)) return
    history.value.push(cloneGraph(graph.value))
    if (history.value.length > 100) history.value.shift()
    future.value = []
    graph.value = next
  }
  const undo = () => {
    const previous = history.value.pop()
    if (previous) {
      future.value.push(cloneGraph(graph.value))
      graph.value = previous
    }
  }
  const redo = () => {
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
    if (dirty.value) {
      try {
        await ElMessageBox.confirm('切换版本将放弃未保存的改动', '切换版本')
      } catch {
        selectedRevisionID.value = revision.value?.id || 0
        return
      }
    }
    revision.value = await fetchWorkflowRevision(workflow.value.id, id)
    graph.value = cloneGraph(revision.value.graph)
    secretChanges.value = []
    history.value = []
    future.value = []
    clean.value = snapshot()
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
      if (!result.valid) error.value = result.issues.map((issue) => issue.message).join('；')
      else ElMessage.success('定义检查通过')
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      validating.value = false
    }
  }
  const save = async () => {
    if (!canEdit.value) return
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
        revisions.value = (await fetchWorkflowRevisions(workflow.value.id)).items
        clean.value = snapshot()
      }
      ElMessage.success('草稿已保存')
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      saving.value = false
    }
  }
  const publish = async () => {
    if (
      !workflow.value ||
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
    } finally {
      publishing.value = false
    }
  }
  const runVisible = ref(false),
    runEntry = ref('main'),
    runInput = reactive<Record<string, unknown>>({}),
    runJSON = ref('{}'),
    publishedGraph = ref<WorkflowGraph>()
  const runSchema = computed(
    () =>
      definitions.value.find(
        (item) =>
          item.type ===
          publishedGraph.value?.nodes.find(
            (node) => node.nodeInstanceId === publishedGraph.value?.entryPoints[runEntry.value]
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
  const openRun = async () => {
    if (!workflow.value) return
    publishedGraph.value = (
      await fetchWorkflowRevision(workflow.value.id, workflow.value.publishedRevisionId)
    ).graph
    runEntry.value = Object.keys(publishedGraph.value.entryPoints)[0] || 'main'
    runVisible.value = true
  }
  const run = async () => {
    if (!workflow.value) return
    running.value = true
    try {
      const input = JSON.parse(runJSON.value)
      const current = await createWorkflowRun(workflow.value.id, {
        revisionId: workflow.value.publishedRevisionId,
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
    min-height: 520px;
  }
</style>
