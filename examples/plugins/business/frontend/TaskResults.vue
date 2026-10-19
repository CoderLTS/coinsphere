<template>
  <section aria-label="业务事项结果">
    <h2>业务事项</h2>
    <ArtSearchBar
      v-model="form"
      :items="[
        {
          key: 'workflowId',
          label: '工作流编号',
          type: 'number',
          props: { min: 0, precision: 0, controlsPosition: 'right', 'aria-label': '工作流编号' }
        }
      ]"
      :show-expand="false"
      @search="apply"
      @reset="reset"
    />
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
  import { ArtSearchBar, useRouteFilters } from '@/plugins/ui'
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
