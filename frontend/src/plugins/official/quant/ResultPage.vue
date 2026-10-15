<template>
  <section aria-label="本次量化结果">
    <ElTag>{{ result.runNode.status }}</ElTag>
    <ElDescriptions :column="1" border
      ><ElDescriptionsItem label="运行">#{{ result.run.id }}</ElDescriptionsItem
      ><ElDescriptionsItem label="节点">{{ result.runNode.nodeInstanceId }}</ElDescriptionsItem
      ><ElDescriptionsItem
        v-for="(value, key) in result.runNode.outputSummary"
        :key="key"
        :label="String(key)"
        >{{ value }}</ElDescriptionsItem
      ></ElDescriptions
    >
    <p v-if="!Object.keys(result.runNode.outputSummary).length">本节点暂无结果</p>
  </section>
</template>
<script setup lang="ts">
  import type { WorkflowRunDetail, WorkflowRunNode } from '@/api/workflows'
  defineProps<{ result: { run: WorkflowRunDetail; runNode: WorkflowRunNode } }>()
</script>
