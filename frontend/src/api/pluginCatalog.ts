import request from '@/utils/http'
export interface ResultPageContribution {
  pageKey: string
  title: string
  componentEntry: string
  scopeSchema: Record<string, unknown>
  filterSchema: Record<string, unknown>
  actions: string[]
}
export interface RunPanelContribution {
  panelKey: string
  title: string
  nodeTypes: string[]
  componentEntry: string
}
export interface PluginCatalogItem {
  id: string
  name: string
  version: string
  pages: { pageKey: string; title: string; icon: string }[]
  headerActions: { pluginId: string; key: string; title: string; icon: string }[]
  resultPages: ResultPageContribution[]
  runPanels: RunPanelContribution[]
}
export const fetchPluginCatalog = () =>
  request.get<{ items: PluginCatalogItem[] }>({ url: '/api/v1/plugins/catalog' })
