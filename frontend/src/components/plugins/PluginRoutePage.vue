<template>
  <PluginPage :plugin-id="pluginId" :page-key="pageKey" :contribution-type="contributionType" />
</template>
<script setup lang="ts">
  import PluginPage from './PluginPage.vue'
  defineProps<{ pluginId: string; pageKey: string; contributionType: 'page' | 'result' }>()
</script>
