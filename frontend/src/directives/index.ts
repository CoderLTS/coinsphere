/** 自定义指令模块：index。 */
import type { App } from 'vue'
import { setupHighlightDirective, type HighlightDirective } from './business/highlight'
import { setupRippleDirective, type RippleDirective } from './business/ripple'

export function setupGlobDirectives(app: App) {
  setupHighlightDirective(app) // 高亮指令
  setupRippleDirective(app) // 水波纹指令
}

export type { HighlightDirective, RippleDirective }
