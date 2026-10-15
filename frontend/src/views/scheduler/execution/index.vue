<template>
  <main class="workflow-execution-page art-full-height">
    <ElForm inline size="small" class="workspace-filters" @submit.prevent="apply">
      <ElFormItem label="工作流编号"
        ><ElInputNumber v-model="form.workflowId" :min="0" :precision="0" aria-label="工作流编号"
      /></ElFormItem>
      <ElFormItem label="触发方式"
        ><ElSelect
          v-model="form.triggerType"
          clearable
          placeholder="全部触发方式"
          aria-label="触发方式"
          ><ElOption
            v-for="(label, value) in triggerLabels"
            :key="value"
            :value="value"
            :label="label" /></ElSelect
      ></ElFormItem>
      <ElFormItem label="执行状态"
        ><ElSelect v-model="form.status" clearable placeholder="全部执行状态" aria-label="执行状态"
          ><ElOption
            v-for="(label, value) in statusLabels"
            :key="value"
            :value="value"
            :label="label" /></ElSelect
      ></ElFormItem>
      <ElFormItem label="时间范围"
        ><ElDatePicker
          v-model="timeRange"
          type="datetimerange"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          range-separator="至"
      /></ElFormItem>
      <ElFormItem label="关键词"
        ><ElInput
          v-model="form.keyword"
          clearable
          :maxlength="200"
          placeholder="节点日志或错误"
          aria-label="关键词"
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
        :show-zebra="false"
        :loading="loading"
        @refresh="loadPageData()"
        ><template #left
          ><ElSpace wrap
            ><strong>运行记录</strong
            ><ElButton @click="router.push('/scheduler/definition')">工作流列表</ElButton></ElSpace
          ></template
        ></ArtTableHeader
      >
      <ArtTable
        :columns="columns"
        :data="runList.records"
        :loading="loading"
        :pagination="pagination"
        :pagination-options="{ pageSizes: [20, 50, 100], layout: 'total, prev, next, sizes' }"
        :stripe="false"
        @pagination:current-change="changePage"
        @pagination:size-change="changeSize"
      />
    </ElCard>
  </main>
</template>
<script setup lang="ts">
  import { ElButton, ElTag } from 'element-plus'
  import { fetchWorkflowRuns, type WorkflowRun } from '@/api/workflows'
  import {
    useRouteFilters,
    useCursorPagination,
    useTableColumns,
    formatDateTime,
    runStatusLabel
  } from '@/plugins/ui'
  defineOptions({ name: 'SchedulerWorkflowExecutionsPage' })
  const router = useRouter()
  const {
    form,
    applied,
    apply,
    reset: resetFilters,
    queryError
  } = useRouteFilters({ workflowId: 0, triggerType: '', status: '', keyword: '', from: '', to: '' })
  const timeRange = computed<[Date, Date] | null>({
    get: () => {
      if (!form.from || !form.to) return null
      const dates: [Date, Date] = [new Date(form.from), new Date(form.to)]
      return dates.every((date) => Number.isFinite(date.getTime())) ? dates : null
    },
    set: (value: Date[] | null) => {
      form.from = value?.[0]?.toISOString() || ''
      form.to = value?.[1]?.toISOString() || ''
    }
  })
  const triggerLabels: Record<string, string> = {
    manual: '手动触发',
    schedule: '定时触发',
    event: '事件触发',
    stream: '流式触发',
    webhook: 'Webhook 触发',
    failure: '失败触发'
  }
  const statusLabels: Record<string, string> = {
    queued: '排队中',
    running: '运行中',
    waiting: '等待中',
    retrying: '等待重试',
    succeeded: '成功',
    failed: '失败',
    cancelled: '已取消'
  }
  const loading = ref(false),
    error = ref('')
  const runList = ref({ records: [] as WorkflowRun[], total: 0, nextCursor: '', hasMore: false })
  const { pagination, requestParams, applyPage, reset, moveTo } = useCursorPagination(20)
  const openDetail = (row: WorkflowRun) =>
    router.push({
      path: `/scheduler/execution/${row.id}/detail`,
      query: { workflowId: row.workflowId }
    })
  const { columns, columnChecks } = useTableColumns<WorkflowRun>(
    () => [
      { prop: 'id', label: '运行编号', width: 100 },
      {
        prop: 'workflowName',
        label: '工作流',
        minWidth: 180,
        formatter: (row) => row.workflowName || '#' + row.workflowId
      },
      {
        prop: 'revisionNumber',
        label: '版本',
        width: 90,
        formatter: (row) => (row.revisionNumber ? 'v' + row.revisionNumber : '#' + row.revisionId)
      },
      {
        prop: 'triggerType',
        label: '触发方式',
        width: 120,
        formatter: (row) => triggerLabels[row.triggerType] || row.triggerType
      },
      {
        prop: 'status',
        label: '状态',
        width: 110,
        formatter: (row) =>
          h(
            ElTag,
            {
              type:
                row.status === 'failed'
                  ? 'danger'
                  : row.status === 'succeeded'
                    ? 'success'
                    : ['waiting', 'retrying', 'running'].includes(row.status)
                      ? 'warning'
                      : 'info',
              effect: 'plain'
            },
            () => runStatusLabel(row.status)
          )
      },
      {
        prop: 'triggeredAt',
        label: '触发时间',
        minWidth: 170,
        formatter: (row) => formatDateTime(row.triggeredAt)
      },
      {
        prop: 'completedAt',
        label: '完成时间',
        minWidth: 170,
        formatter: (row) => formatDateTime(row.completedAt)
      },
      {
        prop: 'duration',
        label: '耗时',
        width: 110,
        formatter: (row) =>
          row.startedAt && row.completedAt
            ? Math.max(0, Date.parse(row.completedAt) - Date.parse(row.startedAt)) + ' ms'
            : '—'
      },
      { prop: 'errorMessage', label: '错误信息', minWidth: 220, showOverflowTooltip: true },
      {
        prop: 'operation',
        label: '操作',
        width: 90,
        fixed: 'right',
        formatter: (row) =>
          h(ElButton, { link: true, type: 'primary', onClick: () => openDetail(row) }, () => '详情')
      }
    ],
    'workflow-runs'
  )
  let request = 0,
    timer: ReturnType<typeof setTimeout> | undefined
  const loadPageData = async (silent = false) => {
    clearTimeout(timer)
    const current = ++request
    if (queryError.value) {
      loading.value = false
      runList.value = { records: [], total: 0, nextCursor: '', hasMore: false }
      return
    }
    if (!silent) loading.value = true
    error.value = ''
    try {
      const result = await fetchWorkflowRuns(undefined, {
        ...requestParams(),
        workflowId: applied.value.workflowId || undefined,
        triggerType: applied.value.triggerType || undefined,
        status: applied.value.status || undefined,
        keyword: applied.value.keyword || undefined,
        from: applied.value.from || undefined,
        to: applied.value.to || undefined
      })
      if (current !== request) return
      runList.value = result
      applyPage(result)
      if (
        result.records.some((row) =>
          ['queued', 'running', 'waiting', 'retrying'].includes(row.status)
        )
      )
        timer = setTimeout(() => loadPageData(true), 3000)
    } catch (cause: any) {
      if (current === request) {
        error.value = cause.message
        runList.value = { records: [], total: 0, nextCursor: '', hasMore: false }
      }
    } finally {
      if (current === request) loading.value = false
    }
  }
  const changePage = (page: number) => {
    if (moveTo(page)) void loadPageData()
  }
  const changeSize = (size: number) => {
    reset(size)
    void loadPageData()
  }
  watch(
    [applied, queryError],
    () => {
      reset()
      void loadPageData()
    },
    { immediate: true }
  )
  onBeforeUnmount(() => {
    request++
    clearTimeout(timer)
  })
</script>
