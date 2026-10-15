<template>
  <main v-loading="loading" class="plugin-result-page">
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <template v-if="page && resultComponent">
      <ElForm inline label-position="top" class="result-filters" @submit.prevent="apply">
        <component
          v-if="configComponent"
          :is="configComponent"
          v-model:scope="scope"
          v-model:filters="filters"
          :scope-schema="page.scopeSchema"
          :filter-schema="page.filterSchema"
        />
        <template v-else
          ><WorkflowSchemaFields
            :schema="page.scopeSchema"
            :config="scope"
            @update="(key, value) => setField(scope, key, value)" /><WorkflowSchemaFields
            :schema="page.filterSchema"
            :config="filters"
            @update="(key, value) => setField(filters, key, value)"
        /></template>
        <ElFormItem
          ><ElButton type="primary" native-type="submit">筛选</ElButton
          ><ElButton @click="reset">重置</ElButton></ElFormItem
        >
      </ElForm>
      <component :is="resultComponent" :context="context" />
    </template>
  </main>
</template>
<script setup lang="ts">
  import type { Component } from 'vue'
  import { fetchPluginCatalog, type ResultPageContribution } from '@/api/pluginCatalog'
  import { loadPluginComponent } from '@/plugins'
  import type { PluginResultContext } from '@/plugins/sdk'
  import WorkflowSchemaFields from '@/components/workflow/WorkflowSchemaFields.vue'
  const props = defineProps<{ pluginId: string; pageKey: string }>()
  const page = ref<ResultPageContribution>(),
    loading = ref(false),
    error = ref('')
  const resultComponent = shallowRef<Component>(),
    configComponent = shallowRef<Component>()
  const scope = ref<Record<string, unknown>>({}),
    filters = ref<Record<string, unknown>>({})
  const context = ref<PluginResultContext>({
    pluginId: props.pluginId,
    pageKey: props.pageKey,
    allowedActions: [],
    scope: {},
    filters: {}
  })
  const setField = (object: Record<string, unknown>, key: string, value: unknown) => {
    if (value === '' || value === undefined || value === null) delete object[key]
    else object[key] = value
  }
  const apply = () => {
    context.value = {
      pluginId: props.pluginId,
      pageKey: props.pageKey,
      allowedActions: page.value?.actions || [],
      scope: structuredClone(toRaw(scope.value)),
      filters: structuredClone(toRaw(filters.value))
    }
  }
  const reset = () => {
    scope.value = {}
    filters.value = {}
    apply()
  }
  let request = 0
  watch(
    () => [props.pluginId, props.pageKey],
    async () => {
      const current = ++request
      loading.value = true
      error.value = ''
      page.value = undefined
      resultComponent.value = undefined
      configComponent.value = undefined
      try {
        const catalog = await fetchPluginCatalog()
        const descriptor = catalog.items
          .find((item) => item.id === props.pluginId)
          ?.resultPages.find((item) => item.pageKey === props.pageKey)
        if (!descriptor) throw new Error('插件结果页面不可用，请检查插件是否已加载')
        const result = await loadPluginComponent(props.pluginId, 'resultPages', props.pageKey)
        const config = descriptor.configComponentEntry
          ? await loadPluginComponent(props.pluginId, 'resultConfigs', props.pageKey)
          : undefined
        if (current !== request) return
        page.value = descriptor
        resultComponent.value = result
        configComponent.value = config
        reset()
      } catch (cause: any) {
        if (current === request) error.value = cause.message
      } finally {
        if (current === request) loading.value = false
      }
    },
    { immediate: true }
  )
</script>
<style scoped>
  .plugin-result-page {
    padding: 20px;
    background: var(--el-bg-color);
    border-radius: 8px;
  }
  .result-filters {
    padding-bottom: 12px;
    margin-bottom: 16px;
    border-bottom: 1px solid var(--el-border-color);
  }
</style>
