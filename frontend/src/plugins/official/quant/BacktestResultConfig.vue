<template>
  <ArtSearchBar
    v-model="form"
    :items="formItems"
    :show-expand="false"
    @search="emit('search')"
    @reset="emit('reset')"
  />
  <ElText v-if="error" type="danger">{{ error }}</ElText>
</template>
<script setup lang="ts">
  import { fetchWorkflows, fetchWorkflow, type WorkflowItem } from '@/api/workflows'
  import { ArtSearchBar, type SearchFormItem } from '@/plugins/ui'
  const form = defineModel<Record<string, any>>({ required: true })
  const emit = defineEmits<{ search: []; reset: [] }>()
  const workflows = ref<WorkflowItem[]>([]),
    error = ref('')
  const formItems = computed<SearchFormItem[]>(() => [
    {
      key: 'workflowId',
      label: '工作流',
      type: 'select',
      props: {
        filterable: true,
        clearable: true,
        remote: true,
        placeholder: '全部工作流',
        emptyValues: [0, null, undefined],
        valueOnClear: 0,
        remoteMethod: search,
        onChange: () => {
          form.value.nodeInstanceId = ''
        },
        options: workflows.value.map((item) => ({ value: item.id, label: item.name }))
      }
    },
    {
      key: 'nodeInstanceId',
      label: '回测节点',
      type: 'input',
      props: {
        clearable: true,
        placeholder: '全部节点（含历史版本）',
        disabled: !form.value.workflowId
      }
    },
    {
      key: 'market',
      label: '市场',
      type: 'select',
      props: {
        clearable: true,
        valueOnClear: '',
        placeholder: '全部市场',
        options: [
          { value: 'spot', label: '现货' },
          { value: 'usdm', label: 'U 本位合约' }
        ]
      }
    },
    {
      key: 'venue',
      label: '交易所',
      type: 'input',
      props: { clearable: true, placeholder: '全部交易所' }
    },
    {
      key: 'instrument',
      label: '交易对',
      type: 'input',
      props: { clearable: true, placeholder: '全部交易对' }
    },
    {
      key: 'limit',
      label: '显示条数',
      type: 'number',
      props: { min: 1, max: 200, precision: 0, controlsPosition: 'right' }
    }
  ])
  let request = 0
  const search = async (keyword = '') => {
    const current = ++request
    try {
      const result = await fetchWorkflows({ keyword, limit: 50 })
      const items = result.records
      if (form.value.workflowId && !items.some((item) => item.id === form.value.workflowId))
        items.unshift(await fetchWorkflow(form.value.workflowId))
      if (current !== request) return
      workflows.value = items
      error.value = ''
    } catch (cause: any) {
      if (current === request) error.value = cause.message
    }
  }
  watch(
    () => form.value.workflowId,
    () => search(),
    { immediate: true }
  )
</script>
