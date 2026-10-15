/** 路由定义模块：staticRoutes。 */
import { AppRouteRecordRaw } from '@/utils/router'

export const staticRoutes: AppRouteRecordRaw[] = [
  {
    path: '/plugins',
    component: () => import('@views/index/index.vue'),
    meta: { title: '插件页面', isHide: true, isHideTab: true },
    children: [
      {
        path: '/plugins/:pluginId/:contributionType(page|result)/:pageKey',
        name: 'PluginPageFallback',
        component: () => import('@/components/plugins/PluginRoutePage.vue'),
        props: (route) => ({
          pluginId: String(route.params.pluginId),
          contributionType: String(route.params.contributionType),
          pageKey: String(route.params.pageKey)
        }),
        meta: { title: '插件页面', isHide: true, isHideTab: true }
      }
    ]
  },
  {
    path: '/auth/login',
    name: 'Login',
    component: () => import('@views/auth/login/index.vue'),
    meta: { title: 'menus.login.title', isHideTab: true }
  },
  {
    path: '/403',
    name: 'Exception403',
    component: () => import('@views/exception/403/index.vue'),
    meta: { title: '403', isHideTab: true }
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'Exception404',
    component: () => import('@views/exception/404/index.vue'),
    meta: { title: '404', isHideTab: true }
  },
  {
    path: '/500',
    name: 'Exception500',
    component: () => import('@views/exception/500/index.vue'),
    meta: { title: '500', isHideTab: true }
  },
  {
    path: '/outside',
    component: () => import('@views/index/index.vue'),
    name: 'Outside',
    meta: { title: 'menus.outside.title' },
    children: [
      {
        path: '/outside/iframe/:path',
        name: 'Iframe',
        component: () => import('@/views/outside/Iframe.vue'),
        meta: { title: 'iframe' }
      }
    ]
  },
  {
    path: '/',
    component: () => import('@views/index/index.vue'),
    name: 'WorkflowEditorShell',
    meta: { title: '工作流编辑器', isHideTab: true, isHide: true },
    children: [
      {
        path: '/scheduler/workflow/create',
        name: 'SchedulerWorkflowDefinitionCreate',
        component: () => import('@views/scheduler/workflow/editor/index.vue'),
        meta: {
          title: '创建工作流定义',
          isHideTab: true,
          isHide: true,
          activePath: '/scheduler/definition'
        }
      },
      {
        path: '/scheduler/workflow/:definitionId/edit',
        name: 'SchedulerWorkflowDefinitionEdit',
        component: () => import('@views/scheduler/workflow/editor/index.vue'),
        meta: {
          title: '编辑工作流定义',
          isHideTab: true,
          isHide: true,
          activePath: '/scheduler/definition'
        }
      },
      {
        path: '/scheduler/workflow/:definitionId/version',
        name: 'SchedulerWorkflowDefinitionVersion',
        redirect: (to) => `/scheduler/workflow/${to.params.definitionId}/edit`,
        meta: {
          title: '编辑工作流定义',
          isHideTab: true,
          isHide: true,
          activePath: '/scheduler/definition'
        }
      },
      {
        path: '/scheduler/execution',
        name: 'SchedulerWorkflowExecutions',
        component: () => import('@views/scheduler/execution/index.vue'),
        meta: {
          title: '历史运行日志',
          isHideTab: true,
          isHide: true,
          activePath: '/scheduler/definition'
        }
      },
      {
        path: '/scheduler/execution/:executionId/detail',
        name: 'SchedulerWorkflowExecutionDetail',
        component: () => import('@views/scheduler/execution/detail/index.vue'),
        meta: {
          title: '执行详情',
          isHideTab: true,
          isHide: true,
          activePath: '/scheduler/definition'
        }
      }
    ]
  }
]
