<template>
  <div v-loading="loading">
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <component v-else-if="page" :is="page" :context="context" />
  </div>
</template>
<script setup lang="ts">
  import type { Component } from 'vue'
  import { fetchPluginCatalog } from '@/api/pluginCatalog'
  import { loadPluginComponent } from '@/plugins'
  import type { PluginPageContext } from '@/plugins/sdk'
  const props = defineProps<{
    pluginId: string
    pageKey: string
    contributionType: 'page' | 'result'
  }>()
  const context = shallowRef<PluginPageContext>({
    pluginId: props.pluginId,
    pageKey: props.pageKey,
    allowedActions: []
  })
  const page = shallowRef<Component>()
  const loading = ref(false),
    error = ref('')
  let request = 0
  watch(
    () => [props.pluginId, props.pageKey, props.contributionType],
    async () => {
      const current = ++request
      loading.value = true
      error.value = ''
      page.value = undefined
      try {
        const catalog = await fetchPluginCatalog()
        const plugin = catalog.items.find((item) => item.id === props.pluginId)
        const contribution = props.contributionType === 'result' ? 'resultPages' : 'pages'
        const descriptor = plugin?.[contribution].find((item) => item.pageKey === props.pageKey)
        const result =
          props.contributionType === 'result'
            ? plugin?.resultPages.find((item) => item.pageKey === props.pageKey)
            : undefined
        if (!descriptor) throw new Error('插件页面不可用，请检查插件是否已加载')
        const component = await loadPluginComponent(props.pluginId, contribution, props.pageKey)
        if (current === request) {
          context.value = {
            pluginId: props.pluginId,
            pageKey: props.pageKey,
            allowedActions: result?.actions || []
          }
          page.value = component
        }
      } catch (cause: any) {
        if (current === request) error.value = cause.message
      } finally {
        if (current === request) loading.value = false
      }
    },
    { immediate: true }
  )
  onErrorCaptured(() => {
    error.value = '插件页面运行失败，请刷新页面后重试'
    return false
  })
</script>
