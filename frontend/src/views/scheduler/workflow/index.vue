<template>
  <main class="workflow-definition-page art-full-height">
    <ArtSearchBar
      v-model="filters"
      :items="formItems"
      :show-expand="false"
      @search="search"
      @reset="reset"
    />
    <ElCard v-loading="mutating" class="art-table-card">
      <div class="workflow-library">
        <aside class="workflow-groups" aria-label="工作流分组">
          <div class="workflow-groups__header"
            ><span>分组</span>
            <ElButton text circle :icon="Plus" aria-label="新建分组" @click="addGroup" />
          </div>
          <div class="workflow-groups__scroll">
            <div class="workflow-groups__fixed">
              <button
                class="workflow-group-filter"
                :class="{ 'is-active': filters.groupId === undefined }"
                @click="selectGroup(undefined)"
              >
                <ElIcon><FolderOpened /></ElIcon
                ><span class="workflow-group-filter__name">全部</span>
              </button>
              <button
                class="workflow-group-filter"
                :class="{ 'is-active': filters.groupId === 0 }"
                @click="selectGroup(0)"
              >
                <ElIcon><Folder /></ElIcon><span class="workflow-group-filter__name">未分组</span>
              </button>
            </div>
            <VueDraggable
              v-model="groups"
              class="workflow-groups__custom"
              handle=".workflow-group-filter__drag"
              :animation="150"
              :disabled="ordering"
              @end="persistGroupOrder"
            >
              <div v-for="(group, index) in groups" :key="group.id" class="workflow-group">
                <ElButton
                  text
                  circle
                  class="workflow-group-filter__drag"
                  :icon="Rank"
                  :aria-label="`拖动分组 ${group.name}`"
                />
                <button
                  class="workflow-group-filter workflow-group-filter--custom"
                  :class="{ 'is-active': filters.groupId === group.id }"
                  @click="selectGroup(group.id)"
                >
                  <ElIcon><Folder /></ElIcon
                  ><span class="workflow-group-filter__name">{{ group.name }}</span>
                </button>
                <ElDropdown trigger="click" @command="groupCommand($event, group, index)">
                  <ElButton text circle :icon="MoreFilled" :aria-label="`管理分组 ${group.name}`" />
                  <template #dropdown
                    ><ElDropdownMenu>
                      <ElDropdownItem command="rename">重命名</ElDropdownItem>
                      <ElDropdownItem command="up" :disabled="index === 0 || ordering"
                        >上移</ElDropdownItem
                      >
                      <ElDropdownItem
                        command="down"
                        :disabled="index === groups.length - 1 || ordering"
                        >下移</ElDropdownItem
                      >
                      <ElDropdownItem command="delete" divided>删除分组</ElDropdownItem>
                    </ElDropdownMenu></template
                  >
                </ElDropdown>
              </div>
            </VueDraggable>
          </div>
        </aside>
        <section class="workflow-table">
          <ArtTableHeader
            :loading="loading"
            :show-zebra="false"
            layout="refresh,fullscreen"
            @refresh="load"
          >
            <template #left
              ><ElSpace wrap>
                <ElButton type="primary" @click="openCreate">创建工作流</ElButton>
                <ElDropdown v-if="selected.length" @command="moveSelected">
                  <ElButton>移至分组（{{ selected.length }}）</ElButton>
                  <template #dropdown
                    ><ElDropdownMenu
                      ><ElDropdownItem :command="0">未分组</ElDropdownItem
                      ><ElDropdownItem
                        v-for="group in groups"
                        :key="group.id"
                        :command="group.id"
                        >{{ group.name }}</ElDropdownItem
                      >
                    </ElDropdownMenu></template
                  >
                </ElDropdown>
              </ElSpace></template
            >
          </ArtTableHeader>
          <ElAlert v-if="error" type="error" :title="error" :closable="false" />
          <ElTable
            v-loading="loading"
            :data="page.records"
            row-key="id"
            @selection-change="selected = $event"
            empty-text="暂无工作流，创建一个工作流开始使用"
          >
            <ElTableColumn type="selection" width="46" />
            <ElTableColumn label="工作流" min-width="220"
              ><template #default="{ row }"
                ><ElButton
                  link
                  type="primary"
                  @click="router.push(`/scheduler/workflow/${row.id}/edit`)"
                  >{{ row.name }}</ElButton
                ><p class="row-description">{{ row.description }}</p></template
              ></ElTableColumn
            >
            <ElTableColumn label="状态" width="110"
              ><template #default="{ row }"
                ><ElTag
                  :type="
                    row.status === 'active' ? 'success' : row.status === 'error' ? 'danger' : 'info'
                  "
                  >{{ statusLabels[row.status] }}</ElTag
                ></template
              ></ElTableColumn
            >
            <ElTableColumn label="版本" min-width="180"
              ><template #default="{ row }"
                >草稿 {{ row.draftRevisionNumber ? 'v' + row.draftRevisionNumber : '—'
                }}<br />已发布
                {{
                  row.publishedRevisionNumber ? 'v' + row.publishedRevisionNumber : '暂无'
                }}</template
              ></ElTableColumn
            >
            <ElTableColumn label="最近运行" min-width="170"
              ><template #default="{ row }"
                ><div v-if="row.latestRunId" class="latest-run"
                  ><ElButton
                    link
                    @click="router.push(`/scheduler/execution/${row.latestRunId}/detail`)"
                    >{{ runStatusLabel(row.latestRunStatus) }}</ElButton
                  ><small>{{ formatDateTime(row.latestRunTriggeredAt) }}</small></div
                ><span v-else>暂无运行</span></template
              ></ElTableColumn
            >
            <ElTableColumn label="更新时间" min-width="170"
              ><template #default="{ row }">{{
                formatDateTime(row.updatedAt)
              }}</template></ElTableColumn
            >
            <ElTableColumn label="分组" min-width="140"
              ><template #default="{ row }"
                ><ElSelect :model-value="row.groupId || 0" @update:model-value="move(row, $event)"
                  ><ElOption label="未分组" :value="0" /><ElOption
                    v-for="group in groups"
                    :key="group.id"
                    :label="group.name"
                    :value="group.id" /></ElSelect></template
            ></ElTableColumn>
            <ElTableColumn label="操作" min-width="280"
              ><template #default="{ row }"
                ><ElSpace wrap
                  ><ElButton link :disabled="!row.publishedRevisionId" @click="lifecycle(row)">{{
                    row.status === 'active' ? '停用' : '启用'
                  }}</ElButton
                  ><ElButton
                    link
                    @click="
                      router.push({ path: '/scheduler/execution', query: { workflowId: row.id } })
                    "
                    >历史运行</ElButton
                  ><ElDropdown @command="workflowCommand($event, row)"
                    ><ElButton link>更多</ElButton
                    ><template #dropdown
                      ><ElDropdownMenu
                        ><ElDropdownItem command="delete" divided
                          >删除工作流</ElDropdownItem
                        ></ElDropdownMenu
                      ></template
                    ></ElDropdown
                  ></ElSpace
                ></template
              ></ElTableColumn
            >
          </ElTable>
          <footer class="page-footer"
            ><span>共 {{ page.total }} 个工作流</span
            ><ElSpace
              ><ElButton :disabled="!cursors.length" @click="previous">上一页</ElButton
              ><ElButton :disabled="!page.hasMore" @click="next">下一页</ElButton></ElSpace
            ></footer
          >
        </section>
      </div>
    </ElCard>
    <ElDialog v-model="createVisible" title="创建工作流" width="min(500px, 94vw)"
      ><ElForm label-position="top"
        ><ElFormItem label="名称"><ElInput v-model="createForm.name" maxlength="120" /></ElFormItem
        ><ElFormItem label="模板"
          ><ElSelect v-model="createForm.templateKey"
            ><ElOption
              v-for="template in templates"
              :key="template.key"
              :label="template.name"
              :value="template.key" /></ElSelect
          ><p>{{
            templates.find((item) => item.key === createForm.templateKey)?.description
          }}</p></ElFormItem
        ></ElForm
      ><template #footer
        ><ElButton @click="createVisible = false">取消</ElButton
        ><ElButton type="primary" :loading="creating" @click="create"
          >创建并编辑</ElButton
        ></template
      ></ElDialog
    >
  </main>
</template>
<script setup lang="ts">
  import { Folder, FolderOpened, Plus, Rank, MoreFilled } from '@element-plus/icons-vue'
  import { VueDraggable } from 'vue-draggable-plus'
  import { ElMessage, ElMessageBox } from 'element-plus'
  import {
    fetchWorkflows,
    fetchWorkflowGroups,
    fetchWorkflowTemplates,
    createWorkflow,
    deleteWorkflow,
    applyWorkflowLifecycle,
    assignWorkflowGroup,
    createWorkflowGroup,
    updateWorkflowGroup,
    deleteWorkflowGroup,
    updateWorkflowGroupOrder,
    type WorkflowSummary,
    type WorkflowTemplate,
    type WorkflowGroup
  } from '@/api/workflows'
  import { runStatusLabel } from '@/components/workflow/status'
  import { formatDateTime } from '@/utils/date'
  import { readRouteFilters } from '@/hooks/core/useRouteFilters'
  const route = useRoute(),
    router = useRouter()
  const loading = ref(false),
    mutating = ref(false),
    creating = ref(false),
    error = ref(''),
    groups = ref<WorkflowGroup[]>([]),
    templates = ref<WorkflowTemplate[]>([])
  const page = ref<Api.Common.PaginatedResponse<WorkflowSummary>>({
    records: [],
    total: 0,
    hasMore: false,
    nextCursor: ''
  })
  const filters = reactive({ keyword: '', status: '', groupId: undefined as number | undefined }),
    applied = reactive({ ...filters })
  const cursor = ref(''),
    cursors = ref<string[]>([])
  const selected = ref<WorkflowSummary[]>([]),
    ordering = ref(false)
  const formItems = [
    {
      label: '工作流名称',
      key: 'keyword',
      type: 'input',
      props: { clearable: true, placeholder: '请输入工作流名称' }
    },
    {
      label: '状态',
      key: 'status',
      type: 'select',
      props: {
        clearable: true,
        options: [
          { label: '未启用', value: 'inactive' },
          { label: '已启用', value: 'active' },
          { label: '异常', value: 'error' }
        ]
      }
    }
  ]
  const selectGroup = (id: number | undefined) => {
    filters.groupId = id
    search()
  }
  const reset = () => {
    Object.assign(filters, { keyword: '', status: '', groupId: undefined })
    search()
  }
  const syncFiltersToURL = () => {
    const query = { ...route.query }
    for (const key of ['keyword', 'status', 'groupId']) delete query[key]
    if (filters.keyword) query.keyword = filters.keyword
    if (filters.status) query.status = filters.status
    if (filters.groupId !== undefined) query.groupId = String(filters.groupId)
    return router.replace({ query })
  }
  const change = async (operation: () => Promise<void>) => {
    if (mutating.value || ordering.value) return
    mutating.value = true
    error.value = ''
    try {
      await operation()
    } catch (cause: any) {
      if (cause !== 'cancel' && cause !== 'close') error.value = cause.message
    } finally {
      mutating.value = false
    }
  }
  const moveSelected = (id: number) =>
    change(async () => {
      await assignWorkflowGroup(
        selected.value.map((item) => item.id),
        id || null
      )
      await load()
    })
  const statusLabels: Record<string, string> = {
    inactive: '未启用',
    active: '已启用',
    error: '异常'
  }
  let requestSequence = 0
  const load = async () => {
    const request = ++requestSequence
    loading.value = true
    selected.value = []
    error.value = ''
    try {
      const result = await fetchWorkflows({ ...applied, cursor: cursor.value, limit: 20 })
      if (request === requestSequence) page.value = result
    } catch (cause: any) {
      if (request === requestSequence) error.value = cause.message
    } finally {
      if (request === requestSequence) loading.value = false
    }
  }
  const search = async () => {
    const previous = route.fullPath
    await syncFiltersToURL()
    if (route.fullPath === previous) applyQuery(route.query)
  }
  const next = () => {
    cursors.value.push(cursor.value)
    cursor.value = page.value.nextCursor
    void load()
  }
  const previous = () => {
    cursor.value = cursors.value.pop() || ''
    void load()
  }
  const move = (row: WorkflowSummary, id: number) =>
    change(async () => {
      await assignWorkflowGroup([row.id], id || null)
      await load()
    })
  const lifecycle = (row: WorkflowSummary) =>
    change(async () => {
      const action = row.status === 'active' ? 'deactivate' : 'activate'
      await ElMessageBox.confirm(
        `${action === 'activate' ? '启用' : '停用'}“${row.name}”？`,
        '工作流状态'
      )
      await applyWorkflowLifecycle(row.id, action)
      await load()
    })
  const remove = (row: WorkflowSummary) =>
    change(async () => {
      await ElMessageBox.confirm(`删除“${row.name}”及其版本和运行记录？`, '删除工作流', {
        type: 'warning'
      })
      await deleteWorkflow(row.id)
      await load()
    })
  const workflowCommand = (command: string, row: WorkflowSummary) => {
    if (command === 'delete') void remove(row)
  }
  const createVisible = ref(false),
    createForm = reactive({ name: '', templateKey: 'blank' })
  const openCreate = async () => {
    try {
      templates.value = (await fetchWorkflowTemplates()).items
      createVisible.value = true
    } catch (cause: any) {
      error.value = cause.message
    }
  }
  const create = async () => {
    if (creating.value) return
    if (!createForm.name.trim()) {
      ElMessage.warning('请输入名称')
      return
    }
    creating.value = true
    try {
      const row = await createWorkflow({
        ...createForm,
        description: '',
        groupId: applied.groupId || null
      })
      createVisible.value = false
      await router.push(`/scheduler/workflow/${row.id}/edit`)
    } catch (cause: any) {
      error.value = cause.message
      ElMessage.error(cause.message)
    } finally {
      creating.value = false
    }
  }
  const namePrompt = async (title: string, value = '') => {
    const result = await ElMessageBox.prompt('名称为 1 至 80 个字符', title, {
      inputValue: value,
      inputValidator: (value) => Boolean(value.trim()) && [...value.trim()].length <= 80
    })
    return result.value.trim()
  }
  const refreshGroups = async () => {
    groups.value = (await fetchWorkflowGroups()).items
  }
  const addGroup = () =>
    change(async () => {
      await createWorkflowGroup(await namePrompt('新建分组'))
      await refreshGroups()
    })
  const renameGroup = (group: WorkflowGroup) =>
    change(async () => {
      await updateWorkflowGroup(group.id, await namePrompt('重命名分组', group.name))
      await refreshGroups()
    })
  const removeGroup = (group: WorkflowGroup) =>
    change(async () => {
      await ElMessageBox.confirm('删除分组后工作流移至未分组', '删除分组')
      await deleteWorkflowGroup(group.id)
      await refreshGroups()
      if (filters.groupId === group.id) filters.groupId = undefined
      await search()
    })
  const persistGroupOrder = async () => {
    if (ordering.value || mutating.value) return
    ordering.value = true
    try {
      groups.value = (await updateWorkflowGroupOrder(groups.value.map((group) => group.id))).items
    } catch (cause: any) {
      error.value = cause.message
      try {
        await refreshGroups()
      } catch (cause: any) {
        error.value = cause.message
      }
    } finally {
      ordering.value = false
    }
  }
  const groupCommand = async (command: string, group: WorkflowGroup, index: number) => {
    if (command === 'rename') await renameGroup(group)
    else if (command === 'delete') await removeGroup(group)
    else {
      const other = command === 'up' ? index - 1 : index + 1
      if (other < 0 || other >= groups.value.length || ordering.value) return
      ;[groups.value[index], groups.value[other]] = [groups.value[other], groups.value[index]]
      await persistGroupOrder()
    }
  }
  const applyQuery = (query: Record<string, unknown>) => {
    try {
      const values = readRouteFilters({ keyword: '', status: '', groupId: 0 }, query)
      const next = { ...values, groupId: query.groupId === undefined ? undefined : values.groupId }
      Object.assign(filters, next)
      Object.assign(applied, next)
      cursor.value = ''
      cursors.value = []
      void load()
    } catch (cause: any) {
      ++requestSequence
      loading.value = false
      error.value = cause.message
      page.value = { records: [], total: 0, hasMore: false, nextCursor: '' }
    }
  }
  watch(() => route.query, applyQuery, { immediate: true })
  onMounted(async () => {
    try {
      await refreshGroups()
    } catch (cause: any) {
      error.value = cause.message
    }
  })
</script>
<style scoped lang="scss">
  .workflow-library {
    display: grid;
    grid-template-columns: 220px minmax(0, 1fr);
    min-height: 420px;
  }

  .workflow-groups {
    min-width: 0;
    padding-right: 16px;
    border-right: 1px solid var(--el-border-color-lighter);
  }

  .workflow-groups__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 40px;
    margin-bottom: 8px;
    font-size: 14px;
    font-weight: 600;
    color: var(--el-text-color-primary);
  }

  .workflow-groups__scroll {
    max-height: calc(100vh - 300px);
    overflow-y: auto;
  }

  .workflow-groups__fixed,
  .workflow-groups__custom {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .workflow-groups__custom {
    margin-top: 4px;
  }

  .workflow-group {
    display: flex;
    gap: 2px;
    align-items: center;
    min-width: 0;
  }

  .workflow-group-filter {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: 8px;
    align-items: center;
    width: 100%;
    min-height: 36px;
    padding: 0 10px;
    font: inherit;
    color: var(--el-text-color-regular);
    text-align: left;
    letter-spacing: 0;
    cursor: pointer;
    background: transparent;
    border: 0;
    border-radius: 4px;
  }

  .workflow-group-filter:hover {
    background: var(--el-fill-color-light);
  }

  .workflow-group-filter.is-active {
    color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
  }

  .workflow-group-filter--custom {
    flex: 1;
    min-width: 0;
  }

  .workflow-group-filter__drag {
    flex: 0 0 28px;
    width: 28px;
    height: 28px;
    cursor: move;
  }

  .workflow-group-filter__name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .workflow-group-filter__count {
    min-width: 20px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    text-align: right;
  }

  .workflow-table {
    min-width: 0;
    padding-left: 16px;
  }

  @media (width <= 900px) {
    .workflow-library {
      display: block;
    }

    .workflow-groups {
      padding-right: 0;
      padding-bottom: 12px;
      margin-bottom: 8px;
      border-right: 0;
      border-bottom: 1px solid var(--el-border-color-lighter);
    }

    .workflow-groups__scroll {
      display: flex;
      gap: 4px;
      max-height: none;
      padding-bottom: 4px;
      overflow-x: auto;
      overflow-y: hidden;
    }

    .workflow-groups__fixed,
    .workflow-groups__custom {
      flex-direction: row;
      flex-shrink: 0;
      margin-top: 0;
    }

    .workflow-group {
      flex: 0 0 auto;
      min-width: 160px;
    }

    .workflow-group-filter {
      min-width: 128px;
    }

    .workflow-table {
      padding-left: 0;
    }
  }

  .row-description {
    margin: 4px 0;
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .latest-run {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    line-height: 18px;
  }
  .latest-run small {
    color: var(--el-text-color-secondary);
    font-size: 11px;
  }
  .page-footer {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding-top: 20px;
  }
</style>
