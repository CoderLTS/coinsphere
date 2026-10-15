<template>
  <component
    v-for="item in actions"
    :is="item.component"
    :key="`${item.pluginId}/${item.key}`"
    :title="item.title"
    :icon="item.icon"
  />
</template>
<script setup lang="ts">
  import type { Component } from 'vue'
  import { fetchPluginCatalog } from '@/api/pluginCatalog'
  import { loadPluginComponent } from '@/plugins'
  import { useUserStore } from '@/store/modules/user'
  const user = useUserStore()
  const actions = shallowRef<
    { pluginId: string; key: string; title: string; icon: string; component: Component }[]
  >([])
  let request = 0
  watch(
    () => user.accessToken,
    async (token) => {
      const current = ++request
      actions.value = []
      if (!token) return
      try {
        const catalog = await fetchPluginCatalog()
        const loaded = await Promise.all(
          catalog.items.flatMap((plugin) =>
            (plugin.headerActions || []).map(async (action) => ({
              ...action,
              component: await loadPluginComponent(plugin.id, 'headerActions', action.key)
            }))
          )
        )
        if (current === request) actions.value = loaded
      } catch {
        // 插件目录加载失败时不展示无法使用的顶栏入口。
      }
    },
    { immediate: true }
  )
</script>
