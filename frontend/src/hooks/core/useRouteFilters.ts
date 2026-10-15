import { shallowReactive, shallowRef, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

type Filters = Record<string, string | number>

export function readRouteFilters<T extends Filters>(
  defaults: T,
  query: Record<string, unknown>
): T {
  const next = { ...defaults }
  for (const key of Object.keys(defaults) as Array<keyof T>) {
    const raw = query[String(key)]
    if (raw === undefined || raw === null) continue
    if (raw === '' && typeof defaults[key] === 'number') continue
    if (typeof raw !== 'string' || raw.length > 4096)
      throw new Error('筛选参数格式无效，请重置筛选')
    if (typeof defaults[key] === 'number') {
      const value = Number(raw)
      if (!/^\d+$/.test(raw) || !Number.isSafeInteger(value))
        throw new Error('筛选编号格式无效，请重置筛选')
      next[key] = value as T[keyof T]
    } else next[key] = raw as T[keyof T]
  }
  return next
}

export function useRouteFilters<T extends Filters>(defaults: T) {
  const route = useRoute(),
    router = useRouter()
  const form = shallowReactive<T>({ ...defaults })
  const applied = shallowRef<T>({ ...defaults })
  const queryError = ref('')
  watch(
    () => route.query,
    (query) => {
      try {
        const next = readRouteFilters(defaults, query)
        Object.assign(form, next)
        queryError.value = ''
        applied.value = next
      } catch (cause: unknown) {
        queryError.value = cause instanceof Error ? cause.message : '筛选参数无效'
      }
    },
    { immediate: true }
  )
  const apply = async () => {
    const query = { ...route.query }
    for (const key of Object.keys(defaults)) {
      delete query[key]
      const value = form[key] ?? defaults[key]
      if (value !== defaults[key]) query[key] = String(value)
    }
    const previous = route.fullPath
    await router.replace({ query })
    // 相同 URL 不触发路由监听，仍允许用户刷新当前筛选。
    if (route.fullPath === previous) {
      try {
        const next = readRouteFilters(defaults, route.query)
        Object.assign(form, next)
        queryError.value = ''
        applied.value = next
      } catch (cause: unknown) {
        queryError.value = cause instanceof Error ? cause.message : '筛选参数无效'
      }
    }
  }
  const reset = () => {
    Object.assign(form, defaults)
    return apply()
  }
  return { form, applied, queryError, apply, reset }
}
