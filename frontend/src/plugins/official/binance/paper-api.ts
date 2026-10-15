import request from '@/utils/http'
export interface PaperResultQuery {
  scope: Record<string, string | number>
  filters: Record<string, string | number>
}

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

export const fetchDirectPaperResult = (context: PaperResultQuery) =>
  request.get<PaperResult>({
    url: '/api/v1/plugins/official.binance/results/paper',
    params: { ...context.scope, ...context.filters }
  })
export const exportDirectPaperResult = (context: PaperResultQuery) =>
  request.request<Blob>({
    url: '/api/v1/plugins/official.binance/results/paper/export',
    params: { ...context.scope, ...context.filters },
    method: 'GET',
    responseType: 'blob',
    rawResponse: true
  })
