<template>
  <ElFormItem label="工作流"
    ><ElSelect
      :model-value="scope.workflowId"
      filterable
      clearable
      remote
      placeholder="全部工作流"
      :remote-method="search"
      @update:model-value="selectWorkflow"
      ><ElOption
        v-for="workflow in workflows"
        :key="workflow.id"
        :label="workflow.name"
        :value="workflow.id" /></ElSelect
  ></ElFormItem>
  <ElFormItem label="模拟交易节点"
    ><ElInput
      :model-value="scope.paperNodeInstanceId"
      clearable
      placeholder="全部节点（含历史版本）"
      :disabled="!scope.workflowId"
      @update:model-value="selectNode"
  /></ElFormItem>
  <WorkflowSchemaFields :schema="filterSchema" :config="filters" @update="setFilter" />
  <ElText v-if="error" type="danger">{{ error }}</ElText>
</template>
<script setup lang="ts">
  import { fetchWorkflows, fetchWorkflow, type WorkflowItem } from '@/api/workflows'
  import { WorkflowSchemaFields } from '@/plugins/ui'
  const props = defineProps<{
    scope: Record<string, any>
    filters: Record<string, any>
    filterSchema: Record<string, any>
  }>()
  const emit = defineEmits<{
    (event: 'update:scope', value: Record<string, unknown>): void
    (event: 'update:filters', value: Record<string, unknown>): void
  }>()
  const workflows = ref<WorkflowItem[]>([]),
    error = ref('')
  let request = 0
  const search = async (keyword = '') => {
    const current = ++request
    try {
      const result = await fetchWorkflows({ keyword, limit: 50 })
      if (current !== request) return
      workflows.value = result.records
      if (
        props.scope.workflowId &&
        !workflows.value.some((item) => item.id === props.scope.workflowId)
      )
        workflows.value.unshift(await fetchWorkflow(props.scope.workflowId))
      error.value = ''
    } catch (cause: any) {
      if (current === request) error.value = cause.message
    }
  }
  const selectWorkflow = (id: number) => emit('update:scope', id ? { workflowId: id } : {})
  const selectNode = (id: string) =>
    emit('update:scope', { ...props.scope, paperNodeInstanceId: id })
  const setFilter = (key: string, value: unknown) =>
    emit('update:filters', { ...props.filters, [key]: value })
  watch(
    () => props.scope.workflowId,
    () => search(),
    { immediate: true }
  )
</script>
