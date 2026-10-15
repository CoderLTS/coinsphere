<template>
  <section class="paper-result" aria-labelledby="paper-result-title">
    <header class="paper-result__header">
      <div>
        <p>币安模拟交易</p>
        <h2 id="paper-result-title">模拟交易结果</h2>
      </div>
      <div class="paper-result__tools">
        <ElButton v-if="canExport" circle title="导出结果" :loading="exporting" @click="download">
          <ArtSvgIcon icon="ri:download-2-line" />
        </ElButton>
        <ElButton circle title="刷新" :loading="loading" @click="load">
          <ArtSvgIcon icon="ri:refresh-line" />
        </ElButton>
      </div>
    </header>

    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElSelect v-if="accounts.length > 1" v-model="accountID" aria-label="账户"
      ><ElOption v-for="item in accounts" :key="item.id" :label="item.id" :value="item.id"
    /></ElSelect>
    <div v-if="account" class="account-strip" aria-label="Paper 账户摘要">
      <div
        ><span>账户</span><strong>{{ account.id }}</strong></div
      >
      <div
        ><span>现金</span><strong>{{ account.cashBalance }}</strong></div
      >
      <div
        ><span>权益</span><strong>{{ account.equity }}</strong></div
      >
      <div
        ><span>持仓</span><strong>{{ account.positions.length }}</strong></div
      >
    </div>

    <section
      v-if="account?.positions.length"
      class="paper-section"
      aria-labelledby="position-title"
    >
      <div class="paper-section__heading">
        <h3 id="position-title">持仓</h3>
        <span>{{ account.positions.length }} 项</span>
      </div>
      <ElTable :data="account.positions" size="small">
        <ElTableColumn label="品种" min-width="150">
          <template #default="scope">
            <strong>{{ scope.row.instrument }}</strong>
            <small>{{ scope.row.market }}</small>
          </template>
        </ElTableColumn>
        <ElTableColumn prop="quantity" label="数量" min-width="130" />
        <ElTableColumn prop="averagePrice" label="均价" min-width="130" />
        <ElTableColumn label="更新时间" min-width="170">
          <template #default="scope">{{ formatTime(scope.row.updatedAt) }}</template>
        </ElTableColumn>
      </ElTable>
    </section>

    <section class="paper-section" aria-labelledby="order-title">
      <div class="paper-section__heading">
        <h3 id="order-title">Paper 订单</h3>
        <span>{{ orders.length }} 条</span>
      </div>
      <ElTable v-loading="loading" :data="orders" size="small" empty-text="暂无 Paper 订单">
        <ElTableColumn label="订单" min-width="190">
          <template #default="scope">
            <strong>{{ scope.row.instrument }}</strong>
            <small>{{ scope.row.clientOrderId }}</small>
          </template>
        </ElTableColumn>
        <ElTableColumn label="方向" width="82">
          <template #default="scope">
            <ElTag
              :type="scope.row.side === 'buy' ? 'success' : 'danger'"
              effect="plain"
              size="small"
            >
              {{ scope.row.side === 'buy' ? '买入' : '卖出' }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn prop="executed" label="成交数量" min-width="120" />
        <ElTableColumn prop="averagePrice" label="成交均价" min-width="120" />
        <ElTableColumn prop="notional" label="名义价值" min-width="120" />
        <ElTableColumn label="状态" width="96">
          <template #default="scope">
            <ElTag
              :type="scope.row.status === 'filled' ? 'success' : 'info'"
              effect="plain"
              size="small"
            >
              {{ statusLabel(scope.row.status) }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="来源运行" width="120"
          ><template #default="{ row }"
            ><ElButton
              v-if="row.runId"
              link
              @click="router.push(`/scheduler/execution/${row.runId}/detail`)"
              >#{{ row.runId }}</ElButton
            ><span v-else>暂无关联运行</span></template
          ></ElTableColumn
        >
        <ElTableColumn label="时间" min-width="170">
          <template #default="scope">{{ formatTime(scope.row.createdAt) }}</template>
        </ElTableColumn>
      </ElTable>
    </section>
  </section>
</template>

<script setup lang="ts">
  import {
    exportDirectPaperResult,
    fetchDirectPaperResult,
    type PaperAccount,
    type PaperOrder,
    type PaperOrderStatus
  } from './paper-api'
  import type { PluginResultContext } from '@/plugins/sdk'
  import { formatDateTime as formatTime } from '@/utils/date'

  const props = defineProps<{ context: PluginResultContext }>()
  const router = useRouter()
  const error = ref(''),
    accountID = ref('')
  const orders = ref<PaperOrder[]>([])
  const accounts = ref<PaperAccount[]>([])
  const loading = ref(false)
  const exporting = ref(false)
  const account = computed(
    () => accounts.value.find((item) => item.id === accountID.value) || accounts.value[0]
  )
  const canExport = computed(() => props.context.allowedActions.includes('export'))
  const statusLabel = (status: PaperOrderStatus) =>
    ({
      new: '待成交',
      partially_filled: '部分成交',
      filled: '已成交',
      canceled: '已撤单',
      rejected: '已拒绝',
      expired: '已过期'
    })[status]

  let request = 0
  const load = async () => {
    const current = ++request
    loading.value = true
    error.value = ''
    try {
      const result = await fetchDirectPaperResult(props.context)
      if (current !== request) return
      orders.value = result.orders
      accounts.value = result.accounts
    } catch (cause: any) {
      if (current === request) {
        error.value = cause.message
        orders.value = []
        accounts.value = []
      }
    } finally {
      if (current === request) loading.value = false
    }
  }

  const download = async () => {
    exporting.value = true
    try {
      const blob = await exportDirectPaperResult(props.context)
      const link = document.createElement('a')
      link.href = URL.createObjectURL(blob)
      link.download = `binance-paper-results.json`
      link.click()
      URL.revokeObjectURL(link.href)
    } catch (cause: any) {
      error.value = cause.message
    } finally {
      exporting.value = false
    }
  }

  watch(() => props.context, load, { immediate: true, deep: true })
</script>

<style scoped>
  .paper-result {
    min-width: 0;
    color: var(--el-text-color-primary);
    letter-spacing: 0;
  }

  .paper-result__header,
  .paper-result__tools,
  .paper-section__heading {
    display: flex;
    align-items: center;
  }

  .paper-result__header,
  .paper-section__heading {
    justify-content: space-between;
  }

  .paper-result__header {
    min-height: 52px;
    padding-bottom: 14px;
    border-bottom: 1px solid var(--el-border-color);
  }

  .paper-result__header p,
  .paper-result__header h2,
  .paper-section h3 {
    margin: 0;
  }

  .paper-result__header p,
  .paper-result small,
  .paper-section__heading span,
  .account-strip span {
    display: block;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }

  .paper-result__header h2 {
    margin-top: 3px;
    font-size: 20px;
    font-weight: 650;
  }

  .paper-result__tools {
    gap: 4px;
  }

  .account-strip {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    margin: 14px 0 20px;
    border-block: 1px solid var(--el-border-color-lighter);
  }

  .account-strip > div {
    min-width: 0;
    padding: 12px 14px;
    border-right: 1px solid var(--el-border-color-lighter);
  }

  .account-strip > div:last-child {
    border-right: 0;
  }

  .account-strip strong {
    display: block;
    margin-top: 4px;
    overflow: hidden;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    text-overflow: ellipsis;
  }

  .paper-section {
    margin-top: 22px;
  }

  .paper-section__heading {
    min-height: 32px;
    margin-bottom: 8px;
  }

  .paper-section h3 {
    font-size: 14px;
    font-weight: 650;
  }

  @media (max-width: 700px) {
    .account-strip {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .account-strip > div:nth-child(2) {
      border-right: 0;
    }
  }
</style>
