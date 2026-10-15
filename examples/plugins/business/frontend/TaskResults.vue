<template>
  <section aria-label="业务事项结果">
    <p>{{ message }}</p>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElButton v-if="context.allowedActions.includes('ack')" :loading="busy" @click="ack"
      >确认事项</ElButton
    >
  </section>
</template>
<script setup lang="ts">
  import { watch, ref } from 'vue'
  import request from '@/utils/http'
  import type { PluginResultContext } from '@/plugins/sdk'
  const props = defineProps<{ context: PluginResultContext }>()
  const message = ref(''),
    error = ref(''),
    busy = ref(false)
  const url = (path: string) => `/api/v1/plugins/${props.context.pluginId}/results/${path}`
  const ack = async () => {
    busy.value = true
    try {
      message.value = (
        await request.post<{ message: string }>({
          url: `${url('ack')}?${new URLSearchParams(Object.entries({ ...props.context.scope, ...props.context.filters }).map(([key, value]) => [key, String(value)])).toString()}`
        })
      ).message
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      busy.value = false
    }
  }
  watch(
    () => props.context,
    async () => {
      try {
        message.value = (
          await request.get<{ message: string }>({
            url: url('tasks'),
            params: { ...props.context.scope, ...props.context.filters }
          })
        ).message
      } catch (cause: any) {
        error.value = cause.message
      }
    },
    { immediate: true, deep: true }
  )
</script>
