<template>
  <ElDialog
    :title="editData?.id ? '编辑菜单' : '新增菜单'"
    :model-value="visible"
    width="620px"
    align-center
    @update:model-value="emit('update:visible', false)"
  >
    <ElForm ref="formRef" :model="form" :rules="rules" label-width="96px">
      <ElFormItem label="页面" prop="page">
        <ElSelect
          v-model="selectedPage"
          :disabled="corePage || Boolean(editData?.children?.length)"
          :loading="loadingPages"
          placeholder="选择目录或插件页面"
          @change="choosePage"
        >
          <ElOption v-if="!editData?.pluginId && !corePage" label="目录" value="" />
          <ElOption v-if="corePage" :label="editData?.meta.title" value="" />
          <ElOption
            v-for="page in pages"
            :key="pageKey(page)"
            :value="pageKey(page)"
            :label="page.title + ' · ' + page.pluginId"
            :disabled="Boolean(page.menuId && page.menuId !== editData?.id)"
          />
        </ElSelect>
        <ElAlert v-if="pageError" :title="pageError" type="error" :closable="false" />
      </ElFormItem>
      <ElFormItem label="父级菜单">
        <ElSelect v-model="form.parentId" clearable placeholder="不选则展示为一级菜单">
          <ElOption
            v-for="option in parents"
            :key="option.id"
            :label="option.title"
            :value="option.id"
          />
        </ElSelect>
      </ElFormItem>
      <ElFormItem label="菜单名称" prop="title"
        ><ElInput v-model.trim="form.title" :maxlength="100"
      /></ElFormItem>
      <ElFormItem label="图标"
        ><ElInput v-model.trim="form.icon" :maxlength="100" placeholder="如：ri:node-tree"
      /></ElFormItem>
      <ElFormItem label="排序"
        ><ElInputNumber v-model="form.sort" :min="0" :max="9999"
      /></ElFormItem>
      <ElFormItem label="启用"><ElSwitch v-model="form.isActive" /></ElFormItem>
      <ElFormItem label="隐藏菜单"><ElSwitch v-model="form.isHidden" /></ElFormItem>
      <template v-if="selectedPage || corePage">
        <ElFormItem label="缓存页面"><ElSwitch v-model="form.keepAlive" /></ElFormItem>
        <ElFormItem label="隐藏标签"><ElSwitch v-model="form.hideTab" /></ElFormItem>
        <ElFormItem label="固定标签"><ElSwitch v-model="form.fixedTab" /></ElFormItem>
      </template>
    </ElForm>
    <template #footer>
      <ElButton @click="emit('update:visible', false)">取消</ElButton>
      <ElButton type="primary" :disabled="loadingPages" @click="submit">确定</ElButton>
    </template>
  </ElDialog>
</template>
<script setup lang="ts">
  import type { FormInstance, FormRules } from 'element-plus'
  import type { AppRouteRecord } from '@/types/router'
  import { fetchMenuPages, type MenuPageOption } from '@/api/system'
  import { formatMenuTitle } from '@/utils/router'
  export interface MenuDialogPayload {
    id?: number
    parentId: number | null
    pluginId: string
    contributionType: string
    pageKey: string
    title: string
    icon: string
    sort: number
    isActive: boolean
    isHidden: boolean
    keepAlive: boolean
    hideTab: boolean
    fixedTab: boolean
  }
  const props = defineProps<{
    visible: boolean
    editData?: AppRouteRecord | null
    menuTree: AppRouteRecord[]
  }>()
  const emit = defineEmits<{
    (event: 'update:visible', visible: boolean): void
    (event: 'submit', payload: MenuDialogPayload): void
  }>()
  const defaults = (): MenuDialogPayload => ({
    parentId: null,
    pluginId: '',
    contributionType: '',
    pageKey: '',
    title: '',
    icon: '',
    sort: 10,
    isActive: true,
    isHidden: false,
    keepAlive: false,
    hideTab: false,
    fixedTab: false
  })
  const form = reactive(defaults()),
    formRef = ref<FormInstance>()
  const selectedPage = ref(''),
    pages = ref<MenuPageOption[]>([]),
    loadingPages = ref(false),
    pageError = ref('')
  const corePage = computed(() =>
    Boolean(
      props.editData && !props.editData.pluginId && props.editData.component !== '/index/index'
    )
  )
  const rules: FormRules = {
    title: [{ required: true, message: '请输入菜单名称', trigger: 'blur' }]
  }
  const pageKey = (page: { pluginId: string; contributionType: string; pageKey: string }) =>
    [page.pluginId, page.contributionType, page.pageKey].join('/')
  const parents = computed(() => {
    const result: { id: number; title: string }[] = []
    const walk = (nodes: AppRouteRecord[], depth = 0) => {
      for (const node of nodes) {
        if (node.id === form.id) continue
        if (node.id && node.component === '/index/index' && !node.pluginId)
          result.push({ id: node.id, title: '　'.repeat(depth) + formatMenuTitle(node.meta.title) })
        walk(node.children || [], depth + 1)
      }
    }
    walk(props.menuTree)
    return result
  })
  const choosePage = () => {
    const page = pages.value.find((page) => pageKey(page) === selectedPage.value)
    form.pluginId = page?.pluginId || ''
    form.contributionType = page?.contributionType || ''
    form.pageKey = page?.pageKey || ''
    if (page && !form.title) form.title = page.title
    if (page && !form.icon) form.icon = page.icon
  }
  watch(
    () => props.visible,
    async (visible) => {
      if (!visible) return
      Object.assign(form, defaults(), { id: undefined })
      const row = props.editData
      if (row)
        Object.assign(form, {
          id: row.id,
          parentId: row.parentId ?? null,
          pluginId: row.pluginId || '',
          contributionType: row.contributionType || '',
          pageKey: row.pageKey || '',
          title: row.meta.title,
          icon: row.meta.icon || '',
          sort: row.meta.sort ?? 10,
          isActive: row.meta.isEnable !== false,
          isHidden: row.meta.isHide === true,
          keepAlive: row.meta.keepAlive === true,
          hideTab: row.meta.isHideTab === true,
          fixedTab: row.meta.fixedTab === true
        })
      selectedPage.value = form.pluginId ? pageKey(form) : ''
      pageError.value = ''
      loadingPages.value = true
      try {
        pages.value = await fetchMenuPages()
        if (row?.pluginId && !pages.value.some((page) => pageKey(page) === selectedPage.value)) {
          pages.value.push({
            pluginId: form.pluginId,
            contributionType: form.contributionType as 'page' | 'result',
            pageKey: form.pageKey,
            title: row.meta.title + '（插件未加载）',
            icon: form.icon,
            menuId: row.id || 0
          })
        }
      } catch {
        pageError.value = '页面目录加载失败，请关闭后重试'
      } finally {
        loadingPages.value = false
      }
      await nextTick()
      formRef.value?.clearValidate()
    }
  )
  const submit = async () => {
    if (pageError.value || !formRef.value) return
    await formRef.value.validate()
    emit('submit', { ...form, parentId: form.parentId || null })
  }
</script>
