<template>
  <section aria-label="本次量化结果">
    <ElTag>{{ runStatusLabel(result.runNode.status) }}</ElTag>
    <ElDescriptions :column="1" border>
      <ElDescriptionsItem label="运行">#{{ result.run.id }}</ElDescriptionsItem>
      <ElDescriptionsItem label="节点">{{ result.runNode.nodeInstanceId }}</ElDescriptionsItem>
      <ElDescriptionsItem v-for="field in fields" :key="field.key" :label="field.title">{{
        field.value
      }}</ElDescriptionsItem>
    </ElDescriptions>
    <p v-if="!Object.keys(result.runNode.outputSummary).length">本节点暂无结果</p>
    <details
      ><summary>原始 JSON</summary
      ><pre>{{ JSON.stringify(result.runNode.outputSummary, null, 2) }}</pre>
    </details>
  </section>
</template>
<script setup lang="ts">
  import type { WorkflowRunDetail, WorkflowRunNode } from '@/api/workflows'
  import { runStatusLabel, schemaOutputFields } from '@/plugins/ui'
  const props = defineProps<{
    result: {
      run: WorkflowRunDetail
      runNode: WorkflowRunNode
      outputSchema?: Record<string, unknown>
    }
  }>()
  const fields = computed(() =>
    schemaOutputFields(props.result.outputSchema || {}, props.result.runNode.outputSummary)
  )
</script>
<style scoped>
  details {
    margin-top: 12px;
  }
  pre {
    max-height: 360px;
    overflow: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
