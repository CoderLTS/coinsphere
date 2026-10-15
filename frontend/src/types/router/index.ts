/** 前端类型定义：index。 */
import { RouteRecordRaw } from 'vue-router'

export interface RouteMeta extends Record<string | number | symbol, unknown> {
  title: string
  icon?: string
  showBadge?: boolean
  showTextBadge?: string
  isHide?: boolean
  isHideTab?: boolean
  link?: string
  isIframe?: boolean
  keepAlive?: boolean
  i18nKey?: string
  i18nTexts?: { zh: string; en: string }
  isFirstLevel?: boolean
  fixedTab?: boolean
  activePath?: string
  isFullPage?: boolean
  parentPath?: string
}

export interface AppRouteRecord extends Omit<RouteRecordRaw, 'meta' | 'children' | 'component'> {
  id?: number
  pluginId?: string | null
  contributionType?: 'page' | 'result' | null
  pageKey?: string | null
  parentId?: number | null
  updatedAt?: string
  meta: RouteMeta
  children?: AppRouteRecord[]
  component?: string | (() => Promise<any>)
}
