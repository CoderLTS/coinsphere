<template>
  <section aria-label="业务事项结果">
    <h2>业务事项</h2>
    <ElForm inline size="small" @submit.prevent="apply">
      <ElFormItem label="工作流编号"
        ><ElInputNumber v-model="form.workflowId" :min="0" :precision="0" aria-label="工作流编号"
      /></ElFormItem>
      <ElFormItem
        ><ElButton type="primary" native-type="submit">查询</ElButton
        ><ElButton @click="reset">全部工作流</ElButton></ElFormItem
      >
    </ElForm>
    <ElAlert
      v-if="queryError || error"
      :title="queryError || error"
      type="error"
      :closable="false"
    />
    <p v-else>{{ message }}</p>
    <ElButton
      v-if="context.allowedActions.includes('ack')"
      :disabled="!!queryError || !message"
      :loading="busy"
      @click="ack"
      >确认事项</ElButton
    >
  </section>
</template>
<script setup lang="ts">
  import { watch, ref, computed } from 'vue'
  import request from '@/utils/http'
  import type { PluginPageContext } from '@/plugins/sdk'
  import { useRouteFilters } from '@/plugins/ui'
  const props = defineProps<{ context: PluginPageContext }>()
  const { form, applied, queryError, apply, reset } = useRouteFilters({
    workflowId: 0
  })
  const params = computed(() =>
    applied.value.workflowId ? { workflowId: applied.value.workflowId } : {}
  )
  const message = ref(''),
    error = ref(''),
    busy = ref(false)
  const url = (path: string) => `/api/v1/plugins/${props.context.pluginId}/results/${path}`
  let requestID = 0
  const load = async () => {
    const current = ++requestID
    message.value = ''
    error.value = ''
    if (queryError.value) return
    try {
      const result = await request.get<{ message: string }>({
        url: url('tasks'),
        params: params.value
      })
      if (current === requestID) message.value = result.message
    } catch (cause: any) {
      if (current === requestID) error.value = cause.message
    }
  }
  const ack = async () => {
    busy.value = true
    error.value = ''
    const current = requestID
    try {
      const query = new URLSearchParams(
        Object.entries(params.value).map(([key, value]) => [key, String(value)])
      )
      const result = await request.post<{ message: string }>({
        url: `${url('ack')}?${query}`
      })
      if (current === requestID) message.value = result.message
    } catch (cause: any) {
      if (current === requestID) error.value = cause.message
    } finally {
      busy.value = false
    }
  }
  watch([applied, queryError], load, { immediate: true })
</script>
