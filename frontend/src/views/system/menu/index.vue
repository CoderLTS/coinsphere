<template>
  <div class="menu-page art-full-height">
    <ArtSearchBar
      v-model="filters"
      :items="[{ key: 'name', label: '菜单名称', type: 'input' }]"
      :show-expand="false"
      @search="keyword = filters.name"
      @reset="reset"
    />
    <ElCard class="art-table-card">
      <ArtTableHeader
        v-model:columns="columnChecks"
        :show-zebra="false"
        :loading="loading"
        @refresh="load"
      >
        <template #left
          ><ElButton @click="edit()">新增菜单</ElButton
          ><ElButton @click="expand">{{ expanded ? '收起' : '展开' }}</ElButton></template
        >
      </ArtTableHeader>
      <ArtTable
        ref="table"
        row-key="id"
        :loading="loading"
        :columns="columns"
        :data="visibleMenus"
        :tree-props="{ children: 'children' }"
      />
      <MenuDialog
        v-model:visible="dialogVisible"
        :edit-data="editData"
        :menu-tree="menus"
        @submit="save"
      />
    </ElCard>
  </div>
</template>
<script setup lang="ts">
  import { ElButton, ElMessageBox } from 'element-plus'
  import { useTableColumns } from '@/hooks/core/useTableColumns'
  import {
    fetchGetManageMenuTree,
    fetchCreateMenu,
    fetchUpdateMenu,
    fetchDeleteMenu
  } from '@/api/system'
  import { useRouter } from 'vue-router'
  import { refreshNavigation } from '@/router/guards/beforeEach'
  import { formatMenuTitle } from '@/utils/router'
  import type { AppRouteRecord } from '@/types/router'
  import MenuDialog, { type MenuDialogPayload } from './modules/menu-dialog.vue'
  defineOptions({ name: 'Menus' })
  const router = useRouter()
  const menus = ref<AppRouteRecord[]>([]),
    loading = ref(false),
    dialogVisible = ref(false),
    editData = ref<AppRouteRecord | null>(null)
  const filters = reactive({ name: '' }),
    keyword = ref(''),
    expanded = ref(false),
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
    try {
      menus.value = await fetchGetManageMenuTree()
    } finally {
      loading.value = false
    }
  }
  const edit = (row?: AppRouteRecord) => {
    editData.value = row ? JSON.parse(JSON.stringify(row)) : null
    dialogVisible.value = true
  }
  const remove = async (row: AppRouteRecord) => {
    await ElMessageBox.confirm(
      `确定删除菜单“${formatMenuTitle(row.meta.title)}”及其子菜单？`,
      '删除菜单',
      { confirmButtonText: '确定', cancelButtonText: '取消' }
    )
    await fetchDeleteMenu(Number(row.id))
    await load()
    await refreshNavigation(router)
  }
  const save = async (payload: MenuDialogPayload) => {
    const { id, ...body } = payload
    if (id) await fetchUpdateMenu(id, body)
    else await fetchCreateMenu(body)
    dialogVisible.value = false
    await load()
    await refreshNavigation(router)
  }
  const expand = () => {
    expanded.value = !expanded.value
    const walk = (rows: AppRouteRecord[]) => {
      for (const row of rows) {
        table.value?.elTableRef?.toggleRowExpansion(row, expanded.value)
        walk(row.children || [])
      }
    }
    walk(visibleMenus.value)
  }
  const { columnChecks, columns } = useTableColumns(() => [
    {
      prop: 'meta.title',
      label: '菜单名称',
      minWidth: 180,
      formatter: (row: AppRouteRecord) => formatMenuTitle(row.meta.title)
    },
    { prop: 'pluginId', label: '所属插件', minWidth: 160 },
    { prop: 'pageKey', label: '页面', minWidth: 160 },
    { prop: 'path', label: '路由地址', minWidth: 160 },
    { prop: 'meta.sort', label: '排序', width: 80 },
    {
      prop: 'meta.isEnable',
      label: '状态',
      width: 90,
      formatter: (row: AppRouteRecord) => (row.meta.isEnable === false ? '停用' : '启用')
    },
    {
      prop: 'operation',
      label: '操作',
      width: 150,
      formatter: (row: AppRouteRecord) =>
        h('div', { class: 'table-actions' }, [
          h(ElButton, { size: 'small', onClick: () => edit(row) }, () => '编辑'),
          h(ElButton, { size: 'small', type: 'danger', onClick: () => remove(row) }, () => '删除')
        ])
    }
  ])
  onMounted(load)
</script>
<style scoped lang="scss">
  .table-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    justify-content: center;
  }

  .table-actions--right {
    justify-content: flex-end;
  }
</style>
