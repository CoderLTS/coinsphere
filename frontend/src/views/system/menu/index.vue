<template>
  <div class="menu-page art-full-height">
    <ArtSearchBar
      v-model="filters"
      :items="[{ key: 'name', label: '菜单名称', type: 'input' }]"
      :show-expand="false"
      @search="keyword = filters.name"
      @reset="reset"
    />
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElCard class="art-table-card">
      <ArtTableHeader
        :show-zebra="false"
        :loading="loading"
        layout="refresh,fullscreen"
        @refresh="load"
      >
        <template #left
          ><ElButton @click="edit()">新增菜单</ElButton
          ><ElButton @click="expand">{{ expanded ? '收起' : '展开' }}</ElButton
          ><span
            >已挂载 {{ pages.filter((page) => page.menuId).length }} /
            {{ pages.length }} 个插件页面</span
          ></template
        >
      </ArtTableHeader>
      <div class="menu-workspace">
        <section class="menu-tree" aria-label="菜单树">
          <ElTable
            ref="table"
            v-loading="loading"
            row-key="id"
            :data="visibleMenus"
            :tree-props="{ children: 'children' }"
            default-expand-all
            highlight-current-row
            @row-click="edit"
          >
            <ElTableColumn label="菜单名称" min-width="170"
              ><template #default="{ row }">{{
                formatMenuTitle(row.meta.title)
              }}</template></ElTableColumn
            >
            <ElTableColumn label="挂载状态" min-width="120"
              ><template #default="{ row }">{{ mountLabel(row) }}</template></ElTableColumn
            >
            <ElTableColumn label="操作" width="70"
              ><template #default="{ row }"
                ><ElButton link type="primary" :disabled="saving" @click.stop="edit(row)"
                  >编辑</ElButton
                ></template
              ></ElTableColumn
            >
          </ElTable>
          <details class="unmounted-pages"
            ><summary>未挂载页面 · {{ unmountedPages.length }}</summary
            ><p
              v-for="page in unmountedPages"
              :key="page.pluginId + '/' + page.contributionType + '/' + page.pageKey"
              >{{ page.title }} · {{ page.pluginId }}</p
            ><p v-if="!unmountedPages.length">全部插件页面已挂载</p></details
          >
        </section>
        <aside class="menu-inspector" aria-label="菜单属性区">
          <MenuDialog
            v-model:visible="dialogVisible"
            :edit-data="editData"
            :menu-tree="menus"
            :saving="saving"
            :error="saveError"
            @submit="save"
          />
          <ElButton
            v-if="dialogVisible && editData?.id"
            link
            type="danger"
            :disabled="saving"
            @click="remove(editData)"
            >删除菜单</ElButton
          >
          <ElEmpty v-if="!dialogVisible" description="选择左侧菜单编辑属性，或新增菜单挂载页面" />
        </aside>
      </div>
    </ElCard>
  </div>
</template>
<script setup lang="ts">
  import { ElMessageBox } from 'element-plus'
  import {
    fetchGetManageMenuTree,
    fetchCreateMenu,
    fetchUpdateMenu,
    fetchDeleteMenu,
    fetchMenuPages,
    type MenuPageOption
  } from '@/api/system'
  import { useRouter } from 'vue-router'
  import { refreshNavigation } from '@/router/guards/beforeEach'
  import { formatMenuTitle } from '@/utils/router'
  import type { AppRouteRecord } from '@/types/router'
  import MenuDialog, { type MenuDialogPayload } from './modules/menu-dialog.vue'
  defineOptions({ name: 'Menus' })
  const router = useRouter()
  const pages = ref<MenuPageOption[]>([]),
    error = ref(''),
    saveError = ref(''),
    saving = ref(false)
  const unmountedPages = computed(() => pages.value.filter((page) => !page.menuId))
  const mountLabel = (row: AppRouteRecord) => {
    if (row.meta.isEnable === false) return '已停用'
    if (
      row.pluginId &&
      !pages.value.some(
        (page) =>
          page.pluginId === row.pluginId &&
          page.contributionType === row.contributionType &&
          page.pageKey === row.pageKey
      )
    )
      return '插件未加载'
    return row.meta.isHide ? '已隐藏' : row.component === '/index/index' ? '目录' : '已挂载'
  }
  const menus = ref<AppRouteRecord[]>([]),
    loading = ref(false),
    dialogVisible = ref(false),
    editData = ref<AppRouteRecord | null>(null)
  const filters = reactive({ name: '' }),
    keyword = ref(''),
    expanded = ref(true),
    table = ref()
  const reset = () => {
    filters.name = ''
    keyword.value = ''
  }
  const visibleMenus = computed(() => {
    const filter = (rows: AppRouteRecord[]): AppRouteRecord[] =>
      rows.flatMap((row) => {
        const children = filter(row.children || [])
        return !keyword.value ||
          formatMenuTitle(row.meta.title).includes(keyword.value) ||
          children.length
          ? [{ ...row, children }]
          : []
      })
    return filter(menus.value)
  })
  const load = async () => {
    loading.value = true
    error.value = ''
    try {
      const [tree, catalog] = await Promise.all([fetchGetManageMenuTree(), fetchMenuPages()])
      menus.value = tree
      pages.value = catalog
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      loading.value = false
    }
  }
  const edit = (row?: AppRouteRecord) => {
    if (saving.value) return
    saveError.value = ''
    editData.value = row ? JSON.parse(JSON.stringify(row)) : null
    dialogVisible.value = true
  }
  const remove = async (row: AppRouteRecord) => {
    if (saving.value) return
    try {
      await ElMessageBox.confirm(
        `确定删除菜单“${formatMenuTitle(row.meta.title)}”及其子菜单？`,
        '删除菜单',
        { confirmButtonText: '确定', cancelButtonText: '取消' }
      )
    } catch {
      return
    }
    saving.value = true
    saveError.value = ''
    try {
      await fetchDeleteMenu(Number(row.id))
      dialogVisible.value = false
      await load()
      await refreshNavigation(router)
    } catch (cause: any) {
      saveError.value = cause.message
    } finally {
      saving.value = false
    }
  }
  const save = async (payload: MenuDialogPayload) => {
    if (saving.value) return
    saving.value = true
    saveError.value = ''
    try {
      const { id, ...body } = payload
      if (id) await fetchUpdateMenu(id, body)
      else await fetchCreateMenu(body)
      dialogVisible.value = false
      await load()
      await refreshNavigation(router)
    } catch (cause: any) {
      saveError.value = cause.message
    } finally {
      saving.value = false
    }
  }
  const expand = () => {
    expanded.value = !expanded.value
    const walk = (rows: AppRouteRecord[]) => {
      for (const row of rows) {
        table.value?.toggleRowExpansion(row, expanded.value)
        walk(row.children || [])
      }
    }
    walk(visibleMenus.value)
  }
  onMounted(load)
</script>
<style scoped lang="scss">
  .menu-workspace {
    display: grid;
    grid-template-columns: minmax(320px, 1fr) minmax(300px, 420px);
    gap: 20px;
  }
  .menu-tree,
  .menu-inspector {
    min-width: 0;
  }
  .menu-inspector {
    padding-left: 20px;
    border-left: 1px solid var(--el-border-color);
  }
  .unmounted-pages {
    margin-top: 16px;
    padding: 12px;
    background: var(--el-fill-color-light);
    border-radius: 6px;
  }
  .unmounted-pages summary {
    cursor: pointer;
  }
  .unmounted-pages p {
    overflow-wrap: anywhere;
  }
  @media (max-width: 900px) {
    .menu-workspace {
      grid-template-columns: minmax(0, 1fr);
    }
    .menu-inspector {
      padding: 16px 0 0;
      border-top: 1px solid var(--el-border-color);
      border-left: 0;
    }
  }
</style>
