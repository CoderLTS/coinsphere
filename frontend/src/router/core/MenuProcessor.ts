/** 动态路由核心模块：MenuProcessor。 */
import type { AppRouteRecord } from '@/types/router'
import { fetchGetMenuList } from '@/api/system'
import { RoutesAlias } from '../routesAlias'

export class MenuProcessor {
  async getMenuList(): Promise<AppRouteRecord[]> {
    const list = await fetchGetMenuList()
    const menuList = this.filterEmptyMenus(this.applyBackendI18nKeys(list))
    return this.normalizeMenuPaths(menuList)
  }

  private applyBackendI18nKeys(menuList: AppRouteRecord[]): AppRouteRecord[] {
    return menuList.map((item) => {
      return {
        ...item,
        meta: {
          ...item.meta,
          title: item.meta?.i18nKey || item.meta?.title || ''
        },
        children: item.children?.length ? this.applyBackendI18nKeys(item.children) : item.children
      }
    })
  }

  private filterEmptyMenus(menuList: AppRouteRecord[]): AppRouteRecord[] {
    return menuList
      .map((item) => {
        if (item.children?.length) {
          return {
            ...item,
            children: this.filterEmptyMenus(item.children)
          }
        }
        return item
      })
      .filter((item) => {
        if (item.component === RoutesAlias.Layout) return Boolean(item.children?.length)
        if (item.children?.length) return true

        if (item.meta?.isIframe === true || item.meta?.link) {
          return true
        }

        return Boolean(
          item.component && item.component !== '' && item.component !== RoutesAlias.Layout
        )
      })
  }

  validateMenuList(menuList: AppRouteRecord[]): boolean {
    return Array.isArray(menuList) && menuList.length > 0
  }

  private normalizeMenuPaths(menuList: AppRouteRecord[], parentPath = ''): AppRouteRecord[] {
    return menuList.map((item) => {
      const fullPath = this.buildFullPath(item.path || '', parentPath)
      const children = item.children?.length
        ? this.normalizeMenuPaths(item.children, fullPath)
        : item.children
      const redirect = item.redirect || this.resolveDefaultRedirect(children)

      return {
        ...item,
        path: fullPath,
        redirect,
        children
      }
    })
  }

  private resolveDefaultRedirect(children?: AppRouteRecord[]): string | undefined {
    if (!children?.length) {
      return undefined
    }

    for (const child of children) {
      if (this.isNavigableRoute(child)) {
        return child.path
      }

      const nestedRedirect = this.resolveDefaultRedirect(child.children)
      if (nestedRedirect) {
        return nestedRedirect
      }
    }

    return undefined
  }

  private isNavigableRoute(route: AppRouteRecord): boolean {
    return Boolean(
      route.path &&
        route.path !== '/' &&
        !route.meta?.link &&
        route.meta?.isIframe !== true &&
        route.component &&
        route.component !== '' &&
        route.component !== RoutesAlias.Layout
    )
  }

  private buildFullPath(path: string, parentPath: string): string {
    if (!path) return ''

    if (path.startsWith('http://') || path.startsWith('https://')) {
      return path
    }

    if (path.startsWith('/')) {
      return path
    }

    if (parentPath) {
      const cleanParent = parentPath.replace(/\/$/, '')
      const cleanChild = path.replace(/^\//, '')
      return `${cleanParent}/${cleanChild}`
    }

    return `/${path}`
  }
}
