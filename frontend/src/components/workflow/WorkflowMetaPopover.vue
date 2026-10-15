<!-- 工作流编辑器页面或组件：WorkflowMetaPopover。 -->
<template>
  <div v-if="visible" class="meta-popover">
    <div class="meta-popover__header">
      <div>
        <strong>基础信息</strong>
      </div>
      <ElButton text @click="$emit('close')">关闭</ElButton>
    </div>

    <ElForm :disabled="readOnly" label-position="top" class="meta-popover__form">
      <ElFormItem class="meta-popover__name" label="工作流名称">
        <ElInput
          v-model="localModel.name"
          maxlength="120"
          placeholder="请输入工作流名称"
          @update:model-value="emitChange"
        />
      </ElFormItem>

      <ElFormItem class="meta-popover__description" label="工作流说明">
        <ElInput
          v-model="localModel.description"
          maxlength="500"
          type="textarea"
          :rows="3"
          placeholder="描述这条工作流的业务职责"
          @update:model-value="emitChange"
        />
      </ElFormItem>
    </ElForm>

    <slot />
    <div class="meta-popover__footer">
      <ElButton @click="$emit('close')">取消</ElButton>
      <ElButton type="primary" @click="$emit('submit', cloneModel(localModel))">应用</ElButton>
    </div>
  </div>
</template>

<script setup lang="ts">
  interface WorkflowEditorMetaForm {
    name: string
    description: string
  }

  interface Props {
    visible: boolean
    readOnly?: boolean
    model: WorkflowEditorMetaForm
  }

  interface Emits {
    (e: 'update:model', value: WorkflowEditorMetaForm): void
    (e: 'submit', value: WorkflowEditorMetaForm): void
    (e: 'close'): void
  }

  const props = defineProps<Props>()
  const emit = defineEmits<Emits>()

  const cloneModel = (value: WorkflowEditorMetaForm): WorkflowEditorMetaForm => ({
    name: value.name,
    description: value.description
  })

  const localModel = reactive<WorkflowEditorMetaForm>(cloneModel(props.model))

  watch(
    () => [props.visible, props.model],
    () => {
      Object.assign(localModel, cloneModel(props.model))
    },
    { deep: true }
  )

  const emitChange = () => {
    emit('update:model', cloneModel(localModel))
  }
</script>

<style scoped lang="scss">
  .meta-popover {
    width: min(560px, calc(100vw - 48px));
    padding: 18px 18px 16px;
    background: color-mix(in srgb, var(--workflow-overlay-bg) 97%, transparent);
    backdrop-filter: blur(18px);
    border: 1px solid var(--workflow-overlay-border-soft);
    border-radius: 8px;
    box-shadow: 0 14px 32px rgb(31 35 48 / 0.12);
  }

  .meta-popover__header {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: 12px;

    strong {
      display: block;
      font-size: 16px;
      color: var(--workflow-overlay-text);
    }

    span {
      display: block;
      margin-top: 4px;
      font-size: 12px;
      line-height: 1.6;
      color: var(--workflow-overlay-muted);
    }
  }

  .meta-popover__form {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0 16px;
  }

  .meta-popover__description {
    grid-column: 1 / -1;
  }

  .meta-popover__name {
    grid-column: 1 / -1;
  }

  .meta-popover__footer {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
    margin-top: 8px;
  }
</style>
