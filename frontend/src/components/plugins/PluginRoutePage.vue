<template>
  <PluginPage v-if="contributionType === 'page'" :plugin-id="pluginId" :page-key="pageKey" />
  <PluginResultPage v-else :plugin-id="pluginId" :page-key="pageKey" />
</template>
<script setup lang="ts">
  import PluginPage from './PluginPage.vue'
  import PluginResultPage from './PluginResultPage.vue'
  defineProps<{ pluginId: string; pageKey: string; contributionType: string }>()
</script>
