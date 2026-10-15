<template>
  <ElAlert v-if="error" :title="error" type="error" :closable="false" />
  <ElFormItem label="模型">
    <ElSelect
      :model-value="config.modelId"
      :loading="loading"
      placeholder="请选择启用的模型"
      @update:model-value="emit('update', 'modelId', $event)"
    >
      <ElOption
        v-for="model in models"
        :key="model.id"
        :value="model.id"
        :label="model.displayName"
      />
    </ElSelect>
  </ElFormItem>
</template>
<script setup lang="ts">
  import { fetchAssistantModels } from './assistant-api'
  defineProps<{ config: Record<string, any> }>()
  const emit = defineEmits<{ (event: 'update', key: string, value: unknown): void }>()
  const models = ref<Api.Assistant.ModelOption[]>([]),
    loading = ref(true),
    error = ref('')
  onMounted(async () => {
    try {
      models.value = await fetchAssistantModels()
    } catch {
      error.value = '模型列表不可用，请检查 AI 插件和模型配置'
    } finally {
      loading.value = false
    }
  })
</script>
