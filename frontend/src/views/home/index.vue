<template>
  <main class="ops-console" v-loading="loading && !overview">
    <header class="console-head">
      <div class="workspace-heading"><h1>运维监控</h1><p>服务健康、请求趋势与运行资源</p></div>
      <div class="head-actions">
        <ElTag
          :type="error || overview?.database.status !== 'healthy' ? 'warning' : 'success'"
          effect="plain"
          >{{
            error
              ? '数据更新失败'
              : !overview
                ? '等待数据'
                : overview.database.status === 'healthy'
                  ? '服务健康'
                  : '依赖异常'
          }}</ElTag
        >
        <span aria-live="polite">{{ refreshedAt ? '更新于 ' + refreshedAt : '等待更新' }}</span>
        <ElSelect v-model="selectedRange" aria-label="监控时间范围" size="small"
          ><ElOption
            v-for="range in [15, 30, 60]"
            :key="range"
            :value="range"
            :label="'近 ' + range + ' 分钟'"
        /></ElSelect>
        <ElButton :icon="Refresh" :loading="loading" size="small" @click="refresh">刷新</ElButton>
      </div>
    </header>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <template v-if="overview">
      <section class="metric-grid" aria-label="所选时间范围请求指标"
        ><article v-for="metric in metrics" :key="metric.label" class="metric-card"
          ><span>{{ metric.label }}</span
          ><strong>{{ metric.value }}</strong
          ><small>{{ metric.caption }}</small></article
        ></section
      >
      <section class="monitor-grid">
        <ElCard shadow="never"
          ><template #header
            ><h2>请求趋势 <small>每分钟请求、失败数与平均延迟</small></h2></template
          ><div
            ref="chartHost"
            class="trend-chart"
            role="img"
            :aria-label="
              '近 ' +
              selectedRange +
              ' 分钟：请求 ' +
              requestCount +
              ' 次，失败 ' +
              failureCount +
              ' 次，平均延迟 ' +
              averageLatency
            "
        /></ElCard>
        <ElCard shadow="never"
          ><template #header><h2>运行资源</h2></template
          ><dl class="resource-list"
            ><div v-for="row in resources" :key="row.label"
              ><dt>{{ row.label }}</dt
              ><dd
                >{{ row.value }}<small>{{ row.note }}</small></dd
              ></div
            ></dl
          ></ElCard
        >
      </section>
      <section class="diagnostics" aria-label="服务诊断">
        <ElCard shadow="never"
          ><template #header><h2>服务诊断</h2></template
          ><ElDescriptions :column="narrow ? 1 : 3" size="small" border
            ><ElDescriptionsItem label="HTTP 服务">{{
              error ? '连接异常，显示上次数据' : '可访问'
            }}</ElDescriptionsItem
            ><ElDescriptionsItem label="PostgreSQL"
              ><ElTag
                :type="overview.database.status === 'healthy' ? 'success' : 'danger'"
                size="small"
                effect="plain"
                >{{ overview.database.status === 'healthy' ? '健康' : '不可用' }}</ElTag
              ></ElDescriptionsItem
            ><ElDescriptionsItem label="数据库结构"
              >v{{ overview.database.schemaVersion }}</ElDescriptionsItem
            ></ElDescriptions
          ></ElCard
        >
        <SystemLogsPanel ref="logsPanel" />
      </section>
    </template>
    <ElEmpty v-else-if="!loading" description="暂时无法读取运行状态，请检查服务连接后刷新" />
  </main>
</template>
<script setup lang="ts">
  import { Refresh } from '@element-plus/icons-vue'
  import { useMediaQuery } from '@vueuse/core'
  import * as echarts from 'echarts'
  import { useSettingStore } from '@/store/modules/setting'
  import { fetchHomeOverview, type HomeOverview } from '@/api/home'
  import { formatDateTime } from '@/utils/date'
  import SystemLogsPanel from './components/SystemLogsPanel.vue'

  defineOptions({ name: 'HomePage' })
  const { isDark } = storeToRefs(useSettingStore())
  const narrow = useMediaQuery('(max-width: 768px)')
  const overview = ref<HomeOverview>(),
    loading = ref(false),
    error = ref(''),
    refreshedAt = ref('')
  const selectedRange = ref(60),
    chartHost = ref<HTMLElement>()
  const logsPanel = ref<{ refresh: () => Promise<void> }>()
  let chart: echarts.ECharts | undefined, observer: ResizeObserver | undefined
  let timer: ReturnType<typeof setInterval> | undefined
  let disposed = false
  const visibleTrend = computed(() =>
    (overview.value?.http.trend || []).slice(-selectedRange.value)
  )
  const requestCount = computed(() =>
    visibleTrend.value.reduce((sum, row) => sum + row.requests, 0)
  )
  const failureCount = computed(() => visibleTrend.value.reduce((sum, row) => sum + row.failed, 0))
  const averageLatency = computed(() =>
    requestCount.value
      ? (
          visibleTrend.value.reduce((sum, row) => sum + row.averageLatencyMs * row.requests, 0) /
          requestCount.value
        ).toFixed(1) + ' ms'
      : '—'
  )
  const metrics = computed(() => [
    {
      label: '请求数',
      value: requestCount.value.toLocaleString(),
      caption: '近 ' + selectedRange.value + ' 分钟'
    },
    {
      label: '失败请求',
      value: failureCount.value.toLocaleString(),
      caption:
        '失败率 ' +
        (requestCount.value ? (failureCount.value / requestCount.value) * 100 : 0).toFixed(1) +
        '%'
    },
    { label: '平均延迟', value: averageLatency.value, caption: '按请求数量加权' },
    {
      label: '并发请求',
      value: overview.value?.http.requestsInFlight.toLocaleString() || '0',
      caption: '当前正在处理'
    }
  ])
  const formatBytes = (value: number) =>
    value >= 1024 ** 3
      ? (value / 1024 ** 3).toFixed(1) + ' GB'
      : (value / 1024 ** 2).toFixed(1) + ' MB'
  const resources = computed(() => {
    const data = overview.value
    if (!data) return []
    return [
      {
        label: '进程内存',
        value: formatBytes(data.process.goMemoryAllocBytes),
        note: '运行时保留 ' + formatBytes(data.process.goMemorySysBytes)
      },
      {
        label: '数据库连接',
        value: data.database.inUse + ' / ' + (data.database.maxOpenConnections || '不限'),
        note: '空闲 ' + data.database.idle + ' · 累计等待 ' + data.database.waitCount
      },
      { label: '运行协程', value: data.process.goroutines.toLocaleString(), note: 'Goroutine' },
      {
        label: '运行时长',
        value:
          Math.floor(data.process.uptimeSeconds / 3600) +
          ' 小时 ' +
          Math.floor((data.process.uptimeSeconds % 3600) / 60) +
          ' 分钟',
        note: '进程启动以来'
      }
    ]
  })
  const renderChart = () => {
    if (!chartHost.value || disposed) return
    chart ||= echarts.init(chartHost.value)
    if (!observer) {
      observer = new ResizeObserver(() => chart?.resize())
      observer.observe(chartHost.value)
    }
    const muted = isDark.value ? '#909AAC' : '#78859A',
      line = isDark.value ? '#353B48' : '#DEE4EE'
    chart.setOption(
      {
        animation: false,
        grid: { left: 48, right: 64, top: 46, bottom: 30 },
        legend: { top: 0, textStyle: { color: muted } },
        tooltip: {
          trigger: 'axis',
          backgroundColor: isDark.value ? '#1A1D25' : '#FFFFFF',
          textStyle: { color: isDark.value ? '#E4E9F2' : '#1F2A3D' }
        },
        xAxis: {
          type: 'category',
          data: visibleTrend.value.map((row) => formatDateTime(row.time).slice(11, 16)),
          axisLabel: {
            color: muted,
            interval: Math.max(0, Math.ceil(visibleTrend.value.length / 6) - 1)
          }
        },
        yAxis: [
          {
            type: 'value',
            minInterval: 1,
            axisLabel: { color: muted },
            splitLine: { lineStyle: { color: line } }
          },
          {
            type: 'value',
            axisLabel: { formatter: '{value} ms', color: muted },
            splitLine: { show: false }
          }
        ],
        series: [
          {
            name: '请求',
            type: 'bar',
            data: visibleTrend.value.map((row) => row.requests),
            barMaxWidth: 12,
            itemStyle: { color: '#356AE6' }
          },
          {
            name: '失败',
            type: 'bar',
            data: visibleTrend.value.map((row) => row.failed),
            barMaxWidth: 12,
            itemStyle: { color: '#D64550' }
          },
          {
            name: '延迟',
            type: 'line',
            yAxisIndex: 1,
            data: visibleTrend.value.map((row) => Number(row.averageLatencyMs.toFixed(1))),
            symbol: 'none',
            lineStyle: { color: '#219E83', width: 2 }
          }
        ]
      },
      true
    )
  }
  const loadOverview = async () => {
    if (loading.value || document.hidden) return
    loading.value = true
    try {
      const result = await fetchHomeOverview()
      if (disposed) return
      overview.value = result
      error.value = ''
      refreshedAt.value = formatDateTime(new Date())
      await nextTick()
      renderChart()
    } catch (cause: any) {
      if (!disposed) error.value = cause.message
    } finally {
      loading.value = false
    }
  }
  const refresh = () => {
    void loadOverview()
    void logsPanel.value?.refresh()
  }
  const schedule = () => {
    clearInterval(timer)
    if (!document.hidden) timer = setInterval(refresh, 15000)
  }
  const visibilityChanged = () => {
    schedule()
    if (!document.hidden) refresh()
  }
  watch([selectedRange, isDark], () => {
    void nextTick(renderChart)
  })
  onMounted(() => {
    void loadOverview()
    schedule()
    document.addEventListener('visibilitychange', visibilityChanged)
  })
  onBeforeUnmount(() => {
    disposed = true
    clearInterval(timer)
    document.removeEventListener('visibilitychange', visibilityChanged)
    observer?.disconnect()
    chart?.dispose()
  })
</script>
<style scoped>
  .ops-console {
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
    font-variant-numeric: tabular-nums;
  }
  .console-head,
  .head-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .head-actions {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
  .head-actions .el-select {
    width: 130px;
  }
  .metric-grid {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 12px;
  }
  .metric-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 16px;
    background: var(--default-box-color);
    border: 1px solid var(--default-border);
    border-radius: 8px;
  }
  .metric-card span,
  .metric-card small {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .metric-card strong {
    font-size: 24px;
    font-weight: 600;
    color: var(--el-text-color-primary);
  }
  .monitor-grid {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(280px, 1fr);
    gap: 16px;
  }
  .monitor-grid > * {
    min-width: 0;
  }
  h2 {
    margin: 0;
    font-size: 16px;
  }
  h2 small {
    margin-left: 8px;
    font-size: 12px;
    font-weight: 400;
    color: var(--el-text-color-secondary);
  }
  .trend-chart {
    height: 280px;
  }
  .resource-list {
    margin: 0;
  }
  .resource-list > div {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .resource-list > div:last-child {
    border-bottom: 0;
  }
  dt {
    color: var(--el-text-color-secondary);
  }
  dd {
    margin: 0;
    text-align: right;
    color: var(--el-text-color-primary);
  }
  dd small {
    display: block;
    margin-top: 4px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
  .diagnostics {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  @media (max-width: 900px) {
    .monitor-grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 600px) {
    .metric-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .head-actions {
      justify-content: flex-start;
    }
    h2 small {
      display: block;
      margin: 6px 0 0;
    }
  }
</style>
