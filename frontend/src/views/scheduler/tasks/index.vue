<template>
  <main class="art-full-height">
    <ElForm inline size="small" class="workspace-filters" @submit.prevent="apply">
      <ElFormItem label="状态"
        ><ElSelect v-model="form.status" clearable placeholder="全部状态" aria-label="待办状态"
          ><ElOption
            v-for="(label, value) in labels"
            :key="value"
            :value="value"
            :label="label" /></ElSelect
      ></ElFormItem>
      <ElFormItem label="工作流编号"
        ><ElInputNumber v-model="form.workflowId" :min="0" :precision="0" aria-label="工作流编号"
      /></ElFormItem>
      <ElFormItem label="运行编号"
        ><ElInputNumber v-model="form.runId" :min="0" :precision="0" aria-label="运行编号"
      /></ElFormItem>
      <ElFormItem
        ><ElButton type="primary" native-type="submit">查询</ElButton
        ><ElButton @click="resetFilters">重置</ElButton></ElFormItem
      >
    </ElForm>
    <ElAlert
      v-if="queryError || error"
      :title="queryError || error"
      type="error"
      :closable="false"
    />
    <ElCard class="art-table-card">
      <ArtTableHeader
        v-model:columns="columnChecks"
        :loading="loading"
        :show-zebra="false"
        @refresh="load"
        ><template #left><strong>人工待办</strong></template></ArtTableHeader
      >
      <ArtTable
        :data="tasks"
        :columns="columns"
        :loading="loading"
        :pagination="pagination"
        :pagination-options="{ pageSizes: [20, 50, 100], layout: 'total, prev, next, sizes' }"
        @pagination:current-change="changePage"
        @pagination:size-change="changeSize"
      />
    </ElCard>
  </main>
</template>
<script setup lang="ts">
  import { ElButton, ElSpace, ElTag, ElMessageBox } from 'element-plus'
  import {
    fetchWorkflowHumanTasks,
    decideWorkflowHumanTask,
    type WorkflowHumanTask
  } from '@/api/workflows'
  import {
    useRouteFilters,
    useCursorPagination,
    useTableColumns,
    formatDateTime
  } from '@/plugins/ui'
  defineOptions({ name: 'WorkflowHumanTasks' })
  const router = useRouter()
  const {
    form,
    applied,
    apply,
    reset: resetFilters,
    queryError
  } = useRouteFilters({ status: 'pending', workflowId: 0, runId: 0 })
  const { pagination, requestParams, applyPage, reset, moveTo } = useCursorPagination(20)
  const labels: Record<string, string> = {
    pending: '待处理',
    approved: '已通过',
    rejected: '已拒绝',
    expired: '已过期',
    superseded: '已被替代'
  }
  const tasks = ref<WorkflowHumanTask[]>([]),
    loading = ref(false),
    error = ref(''),
    deciding = ref(0)
  const { columns, columnChecks } = useTableColumns<WorkflowHumanTask>(
    () => [
      {
        prop: 'workflowName',
        label: '工作流',
        minWidth: 160,
        formatter: (row) => row.workflowName || '#' + row.workflowId
      },
      {
        prop: 'nodeName',
        label: '节点',
        minWidth: 140,
        formatter: (row) => row.nodeName || row.nodeInstanceId
      },
      { prop: 'prompt', label: '待办内容', minWidth: 240, showOverflowTooltip: true },
      {
        prop: 'status',
        label: '状态',
        width: 110,
        formatter: (row) =>
          h(
            ElTag,
            { type: row.status === 'pending' ? 'warning' : 'info', effect: 'plain' },
            () => labels[row.status]
          )
      },
      {
        prop: 'expiresAt',
        label: '截止时间',
        minWidth: 170,
        formatter: (row) => formatDateTime(row.expiresAt)
      },
      {
        prop: 'runId',
        label: '来源运行',
        width: 110,
        formatter: (row) =>
          h(
            ElButton,
            {
              link: true,
              onClick: () => router.push('/scheduler/execution/' + row.runId + '/detail')
            },
            () => '#' + row.runId
          )
      },
      {
        prop: 'operation',
        label: '操作',
        width: 150,
        fixed: 'right',
        formatter: (row) =>
          row.status === 'pending'
            ? h(ElSpace, {}, () => [
                h(
                  ElButton,
                  {
                    link: true,
                    type: 'primary',
                    loading: deciding.value === row.id,
                    disabled: deciding.value !== 0,
                    onClick: () => decide(row, 'approve')
                  },
                  () => '通过'
                ),
                h(
                  ElButton,
                  {
                    link: true,
                    type: 'danger',
                    disabled: deciding.value !== 0,
                    onClick: () => decide(row, 'reject')
                  },
                  () => '拒绝'
                )
              ])
            : '—'
      }
    ],
    'human-tasks'
  )
  let request = 0
  const load = async () => {
    const current = ++request
    if (queryError.value) {
      loading.value = false
      tasks.value = []
      return
    }
    loading.value = true
    error.value = ''
    try {
      const result = await fetchWorkflowHumanTasks({
        ...requestParams(),
        status: applied.value.status,
        workflowId: applied.value.workflowId || undefined,
        runId: applied.value.runId || undefined
      })
      if (current === request) {
        tasks.value = result.records
        applyPage(result)
      }
    } catch (cause: any) {
      if (current === request) {
        error.value = cause.message
        tasks.value = []
      }
    } finally {
      if (current === request) loading.value = false
    }
  }
  const decide = async (task: WorkflowHumanTask, action: 'approve' | 'reject') => {
    if (deciding.value) return
    try {
      await ElMessageBox.confirm(
        (action === 'approve' ? '通过' : '拒绝') + '此待办？',
        '处理待办',
        { confirmButtonText: '确定', cancelButtonText: '取消' }
      )
    } catch {
      return
    }
    deciding.value = task.id
    try {
      await decideWorkflowHumanTask(task.id, action)
      await load()
    } catch (cause: any) {
      await load()
      error.value = cause.message + '；待办已刷新，请确认当前状态'
    } finally {
      deciding.value = 0
    }
  }
  const changePage = (page: number) => {
    if (moveTo(page)) void load()
  }
  const changeSize = (size: number) => {
    reset(size)
    void load()
  }
  watch(
    [applied, queryError],
    () => {
      reset()
      void load()
    },
    { immediate: true }
  )
  onBeforeUnmount(() => request++)
</script>
