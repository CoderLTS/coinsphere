# CoinSphere 代码结构

系统为个人 Go/Vue 模块化单体，Core 1.0.0 / SDK 1 / Graph 3；设计见[架构](architecture/overview.md)，语义见[契约](contracts/README.md)。

| 目录/文件 | 修改入口 |
| --- | --- |
| backend/main.go | 配置、只读 schema 验证、插件加载、一次账号/导航 seed、执行器与 HTTP |
| backend/cmd/migrate | g5 Core/官方插件空库基线 CLI |
| backend/cmd/coinsphere | 保持源码插件 validate/install/upgrade/uninstall/purge-data |
| backend/internal/api | /api/v1 输入边界、会话、Scope 路由、WebSocket、Problem Details |
| backend/internal/service/auth.go、authorization.go | 单账号、会话撤销、资源存在性及写事务复验 |
| backend/internal/service/system.go、plugin_host.go | 页面目录/多级菜单、Host 通用摘要/查询/停用草稿 |
| backend/internal/service/workflow*.go | 图/修订/发布/Secret、队列、租约、池、Loop、审批、状态/检查点、事件与调度 |
| backend/workflow/graph | Graph 3、Schema、CEL、绑定与纯计算 |
| backend/internal/service/plugin_results.go、inbox.go | 当前插件页面请求范围、个人收件箱 |
| backend/internal/migration/generation5 | public 空库基线与 g5 版本账本 |
| backend/plugin/sdk | Action/Trigger、System/Workflow/Result Scope、页面/动作/headerActions 等贡献与 Host |
| backend/plugin/contracts/trading | 金融 provider/strategy 契约，逐帧逻辑不进入 SDK |
| backend/plugin/official/ai | 模型配置、助手会话/SSE/草稿、modelId 节点、plugin_ai migration |
| backend/plugin/official/quant、binance、notification、qq、connector | 领域节点/协议/表/页面，Binance 只公共数据/Paper |
| backend/internal/pluginbuild、pluginlifecycle、pluginregistry | 静态表、引用保护、失败安装恢复 |
| backend/version/builtin.json | 官方版本、真实依赖与贡献唯一目录 |
| frontend/src/components/workflow | 通用 Schema、画布、绑定、Loop、运行/审批交互 |
| frontend/src/components/core | 实际使用的 ArtDesignPro 布局、主题、表格、图表和表单组件；不保留示例组件库 |
| frontend/src/components/plugins、router/core | 插件页面/结果/顶栏装配，多级导航刷新、路由可用性与稳定页面地址 |
| frontend/src/plugins | Vue SDK、官方贡献、生成注册表 |
| examples/plugins/business | 真实非金融 Go/Vue 示例，CI 编译/装配 |
| backend/internal/testdb、*_test.go、frontend/e2e | 隔离合成行为与三浏览器关键路径 |
| deploy/production、scripts/release | 单服务固定 digest、候选隔离与定向恢复 |

安装源码位于 backend/plugin/installed 和 frontend/src/plugins/installed，不手工维护生成表。领域表由插件 schema/migration 拥有；Core Cleanup 使用同事务。公共契约、migration、共享配置/依赖保持单一维护者，最终只读复审由主 Agent 完成。动态进度及验证证据保存在 GitHub。

前端导航只读取后端个人菜单，不提供前端权限模式或第二份静态菜单。路由可用性检查用于导航恢复，不代替后端会话和资源检查。公开 SDK、契约测试工具及插件 CLI 由插件调用，不以主应用引用数为由删除。
