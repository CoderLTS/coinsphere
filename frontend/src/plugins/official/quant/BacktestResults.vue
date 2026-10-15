<template>
  <section v-loading="loading" aria-label="回测结果">
    <header class="result-header"><h2>回测结果</h2><ElButton @click="load">刷新</ElButton></header>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElTable :data="backtests" empty-text="暂无回测结果" @row-click="select">
      <ElTableColumn label="范围" min-width="180"
        ><template #default="{ row }"
          ><strong>{{ row.instrument }}</strong
          ><p>{{ row.market }} · {{ row.interval }}</p></template
        ></ElTableColumn
      >
      <ElTableColumn prop="finalEquity" label="最终权益" min-width="120" />
      <ElTableColumn label="收益率" width="120"
        ><template #default="{ row }"
          >{{ decimalPercent(row.totalReturn) }}%</template
        ></ElTableColumn
      >
      <ElTableColumn label="最大回撤" width="120"
        ><template #default="{ row }"
          >{{ decimalPercent(row.maxDrawdown) }}%</template
        ></ElTableColumn
      >
      <ElTableColumn prop="tradeCount" label="成交次数" width="110" />
      <ElTableColumn label="来源运行" width="120"
        ><template #default="{ row }"
          ><ElButton
            v-if="row.runId"
            link
            @click.stop="router.push(`/scheduler/execution/${row.runId}/detail`)"
            >#{{ row.runId }}</ElButton
          ><span v-else>暂无关联运行</span></template
        ></ElTableColumn
      >
      <ElTableColumn label="完成时间" min-width="180"
        ><template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template></ElTableColumn
      >
      <ElTableColumn label="分析" width="90"
        ><template #default="{ row }"
          ><ElButton link @click.stop="select(row)">详情</ElButton></template
        ></ElTableColumn
      >
    </ElTable>
    <ElDialog v-model="detailVisible" title="回测分析" width="95%" destroy-on-close
      ><BacktestAnalysis v-if="selected" :backtest="selected" :context="context"
    /></ElDialog>
  </section>
</template>
<script setup lang="ts">
  import type { PluginResultContext } from '@/plugins/sdk'
  import { fetchDirectBacktests, type QuantBacktest } from './api'
  import { decimalPercent } from './decimal'
  import { formatDateTime } from '@/utils/date'
  import BacktestAnalysis from './BacktestAnalysis.vue'
  const props = defineProps<{ context: PluginResultContext }>()
  const router = useRouter(),
    backtests = ref<QuantBacktest[]>([]),
    selected = ref<QuantBacktest>()
  const loading = ref(false),
    error = ref(''),
    detailVisible = ref(false)
  const select = (row: QuantBacktest) => {
    selected.value = row
    detailVisible.value = true
  }
  let request = 0
  const load = async () => {
    const current = ++request
    loading.value = true
    error.value = ''
    try {
      const result = await fetchDirectBacktests(props.context)
      if (current === request) backtests.value = result.items
    } catch (cause: any) {
      if (current === request) {
        error.value = cause.message
        backtests.value = []
      }
    } finally {
      if (current === request) loading.value = false
    }
  }
  watch(() => props.context, load, { immediate: true, deep: true })
</script>
<style scoped>
  .result-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 16px;
  }
  .result-header h2 {
    margin: 0;
  }
</style>
