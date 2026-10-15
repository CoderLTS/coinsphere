<template>
  <div v-loading="loading">
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <component v-else-if="page" :is="page" />
  </div>
</template>
<script setup lang="ts">
  import type { Component } from 'vue'
  import { fetchPluginCatalog } from '@/api/pluginCatalog'
  import { loadPluginComponent } from '@/plugins'
  const props = defineProps<{ pluginId: string; pageKey: string }>()
  const page = shallowRef<Component>()
  const loading = ref(false),
    error = ref('')
  let request = 0
  watch(
    () => [props.pluginId, props.pageKey],
    async () => {
      const current = ++request
      loading.value = true
      error.value = ''
      page.value = undefined
      try {
        const catalog = await fetchPluginCatalog()
        if (
          !catalog.items
            .find((item) => item.id === props.pluginId)
            ?.pages?.some((item) => item.pageKey === props.pageKey)
        )
          throw new Error('插件页面不可用，请检查插件是否已加载')
        const component = await loadPluginComponent(props.pluginId, 'pages', props.pageKey)
        if (current === request) page.value = component
      } catch (cause: any) {
        if (current === request) error.value = cause.message
      } finally {
        if (current === request) loading.value = false
      }
    },
    { immediate: true }
  )
</script>
