import request from '@/utils/http'
import type { PluginResultContext } from '@/plugins/sdk'

export type PaperOrderStatus =
  | 'new'
  | 'partially_filled'
  | 'filled'
  | 'canceled'
  | 'rejected'
  | 'expired'

export interface PaperOrder {
  id: number
  workflowId: number
  nodeInstanceId: string
  runId?: number
  account: string
  clientOrderId: string
  market: 'spot' | 'usdm'
  instrument: string
  side: 'buy' | 'sell'
  quantity: string
  executed: string
  averagePrice: string
  notional: string
  status: PaperOrderStatus
  createdAt: string
  updatedAt: string
}

export interface PaperPosition {
  market: 'spot' | 'usdm'
  instrument: string
  quantity: string
  averagePrice: string
  updatedAt: string
}

export interface PaperAccount {
  id: string
  cashBalance: string
  equity: string
  positions: PaperPosition[]
}

export interface PaperResult {
  orders: PaperOrder[]
  accounts: PaperAccount[]
}

export const fetchDirectPaperResult = (context: PluginResultContext) =>
  request.get<PaperResult>({
    url: '/api/v1/plugins/official.binance/results/paper',
    params: { ...context.scope, ...context.filters }
  })
export const exportDirectPaperResult = (context: PluginResultContext) =>
  request.request<Blob>({
    url: '/api/v1/plugins/official.binance/results/paper/export',
    params: { ...context.scope, ...context.filters },
    method: 'GET',
    responseType: 'blob',
    rawResponse: true
  })
