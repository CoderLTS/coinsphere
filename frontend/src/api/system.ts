/** 前端接口封装：system。 */
import request from '@/utils/http'
import { AppRouteRecord } from '@/types/router'

export function fetchGetMenuList() {
  return request.get<AppRouteRecord[]>({
    url: '/api/v1/system/menus'
  })
}

export interface MenuPageOption {
  pluginId: string
  contributionType: 'page' | 'result'
  pageKey: string
  title: string
  icon: string
  menuId: number
}
export function fetchMenuPages() {
  return request.get<MenuPageOption[]>({ url: '/api/v1/system/menus/pages' })
}

export function fetchGetManageMenuTree() {
  return request.get<AppRouteRecord[]>({
    url: '/api/v1/system/menus/manage-tree'
  })
}

export function fetchGetMenuI18nDict() {
  return request.get<Api.System.MenuI18nDict>({
    url: '/api/v1/system/i18n-dictionaries',
    params: { scope: 'menu' }
  })
}

export function fetchUploadAvatarAsset(file: File) {
  const formData = new FormData()
  formData.append('avatar', file)
  return request.post<{ url: string }>({
    url: '/api/v1/system/uploads/avatars',
    data: formData,
    showSuccessMessage: true
  })
}

export function fetchUploadUserAvatar(file: File) {
  return fetchUploadAvatarAsset(file)
}

export function fetchCreateMenu(params: Record<string, any>) {
  return request.post<{ id: number }>({
    url: '/api/v1/system/menus',
    params,
    showSuccessMessage: true
  })
}

export function fetchUpdateMenu(menuId: number, params: Record<string, any>) {
  return request.put<{ id: number }>({
    url: `/api/v1/system/menus/${menuId}`,
    params,
    showSuccessMessage: true
  })
}

export function fetchDeleteMenu(menuId: number) {
  return request.del<void>({
    url: `/api/v1/system/menus/${menuId}`,
    showSuccessMessage: true
  })
}

export function fetchGetInstalledPlugins() {
  return request.get<Api.System.InstalledPlugin[]>({
    url: '/api/v1/system/plugins'
  })
}

export function fetchGetSystemLogs(params: Api.System.SystemLogSearchParams) {
  return request.get<Api.System.SystemLogList>({
    url: '/api/v1/system/logs',
    params
  })
}

export function fetchGetSystemLogRuntime() {
  return request.get<Api.System.SystemLogRuntimeStatus>({
    url: '/api/v1/system/logs/runtime'
  })
}

export function fetchUpdateSystemLogRuntime(params: Api.System.SystemLogSettingsPayload) {
  return request.put<Api.System.SystemLogRuntimeStatus>({
    url: '/api/v1/system/logs/runtime',
    params,
    showSuccessMessage: true
  })
}

export function fetchGetOutboundProxies() {
  return request.get<Api.System.OutboundProxyItem[]>({
    url: '/api/v1/system/proxies'
  })
}

export function fetchCreateOutboundProxy(params: Api.System.OutboundProxyUpsertPayload) {
  return request.post<Api.System.OutboundProxyItem>({
    url: '/api/v1/system/proxies',
    params,
    showSuccessMessage: true
  })
}

export function fetchUpdateOutboundProxy(
  proxyId: number,
  params: Api.System.OutboundProxyUpsertPayload
) {
  return request.put<Api.System.OutboundProxyItem>({
    url: `/api/v1/system/proxies/${proxyId}`,
    params,
    showSuccessMessage: true
  })
}

export function fetchSetOutboundProxyEnabled(proxyId: number, isEnabled: boolean) {
  return request.request<Api.System.OutboundProxyItem>({
    url: `/api/v1/system/proxies/${proxyId}`,
    method: 'PATCH',
    data: { isEnabled },
    showSuccessMessage: true
  })
}

export function fetchDeleteOutboundProxy(proxyId: number) {
  return request.del<{ id: number }>({
    url: `/api/v1/system/proxies/${proxyId}`,
    showSuccessMessage: true
  })
}

export function fetchValidateOutboundProxy(proxyId: number) {
  return request.post<Api.System.OutboundProxyValidationResult>({
    url: `/api/v1/system/proxies/${proxyId}/validations`,
    showSuccessMessage: false
  })
}
