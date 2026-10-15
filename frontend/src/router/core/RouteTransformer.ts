import { h } from 'vue'
import { RouterView, type RouteRecordRaw } from 'vue-router'
import type { AppRouteRecord } from '@/types/router'
import { ComponentLoader } from './ComponentLoader'

export class RouteTransformer {
  constructor(private componentLoader: ComponentLoader) {}

  transform(route: AppRouteRecord, depth = 0): RouteRecordRaw {
    const { children, component, ...record } = route
    const directory = component === '/index/index'
    const page = { ...record, component: this.componentLoader.load(component as string) }
    if (directory) {
      return {
        ...record,
        component:
          depth === 0 ? this.componentLoader.loadLayout() : { render: () => h(RouterView) },
        children: (children || []).map((child) => this.transform(child, depth + 1))
      } as RouteRecordRaw
    }
    if (depth > 0) return page as RouteRecordRaw
    // A top-level page has its own layout; the page URL stays independent of menu placement.
    return {
      path: route.path,
      component: this.componentLoader.loadLayout(),
      meta: { ...route.meta, isFirstLevel: true },
      children: [{ ...page, path: '', meta: { ...route.meta, isFirstLevel: true } }]
    } as RouteRecordRaw
  }
}
