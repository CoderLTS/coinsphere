<template>
  <ElTooltip :content="title" placement="bottom">
    <ArtIconButton
      :icon="icon"
      :aria-label="title"
      role="button"
      tabindex="0"
      @click="chat?.open()"
      @keydown.enter.prevent="chat?.open()"
      @keydown.space.prevent="chat?.open()"
    />
  </ElTooltip>
  <ChatWindow ref="chat" />
</template>
<script setup lang="ts">
  import ChatWindow from './ChatWindow.vue'
  defineProps<{ title: string; icon: string }>()
  const chat = ref<InstanceType<typeof ChatWindow>>()
</script>
