export const pages = { models: () => import('./ModelsPage.vue') }
export const headerActions = { chat: () => import('./HeaderChat.vue') }
export const runPanels = {
  calls: () => import('./ResultPage.vue')
}

export const nodeEditors = { model: () => import('./ModelNodeEditor.vue') }
