import type { AppRouteRecord } from '@/types/router'

export class RouteAvailability {
  static isAvailable(targetPath: string, menuList: AppRouteRecord[]): boolean {
    // 根路径始终允许访问
    if (targetPath === '/') {
      return true
    }

    return this.matchRoute(targetPath, menuList)
  }

  static matchRoute(targetPath: string, routes: AppRouteRecord[]): boolean {
    if (!Array.isArray(routes) || routes.length === 0) {
      return false
    }

    for (const route of routes) {
      if (!route.path) {
        continue
      }

      const routePath = route.path.startsWith('/') ? route.path : `/${route.path}`

      if (
        routePath === targetPath ||
        this.isDynamicRouteMatch(targetPath, routePath) ||
        targetPath.startsWith(`${routePath}/`)
      ) {
        return true
      }

      if (route.children?.length && this.matchRoute(targetPath, route.children)) {
        return true
      }
    }

    return false
  }

  static isDynamicRouteMatch(targetPath: string, routePath: string): boolean {
    if (!routePath.includes(':')) {
      return false
    }

    const pattern = routePath
      .replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
      .replace(/:([^/]+)/g, '[^/]+')
      .replace(/\\\*/g, '.*')

    return new RegExp(`^${pattern}$`).test(targetPath)
  }

  static validatePath(
    targetPath: string,
    menuList: AppRouteRecord[],
    homePath: string = '/'
  ): { path: string; isAvailable: boolean } {
    const isAvailable = this.isAvailable(targetPath, menuList)

    if (isAvailable) {
      return { path: targetPath, isAvailable: true }
    }

    return { path: homePath, isAvailable: false }
  }
}
