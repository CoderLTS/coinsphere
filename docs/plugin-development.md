# CoinSphere 插件开发指南

当前为 Core 1.0.0 / SDK 1 / manifest schemaVersion 1。插件继续是经过审查的可信 Go/Vue 源码目录，通过现有 CLI 安装、独立 migration、静态注册表与应用重建接入，无商店或运行时动态代码。安装版本与编译版本精确匹配才加载；插件同进程，SDK 不是恶意代码沙箱。

## 清单与注册

[业务事项示例](../examples/plugins/business/coinsphere-plugin.json)包含实际 Go 节点、WorkflowScope 路由、ResultPage、ack 动作、Vue RunPanel 及独立 migration，不需要 Core 业务分支。ack 仅确认当前响应；真实领域写入由插件事务和幂等键负责。

```text
business/
├─ coinsphere-plugin.json
├─ backend/go.mod、plugin.go
├─ frontend/index.ts、TaskPanel.vue、TaskResults.vue
└─ migrations/00001_business.sql
```

manifest 声明 id/name/version/sdkMajor/requiresCore/backend/frontend/migrations/contributes，可用 requiresPlugins 声明真实 SemVer 依赖；不声明 permissions。路径必须在根内，拒绝绝对路径、反斜杠和符号链接逃逸。backend.module 与自身 go.mod 一致，生成主仓库 require/replace，不手工改生成注册表。frontend 入口导出贡献对象，不能只 default export。

贡献包括 nodes、triggers、apiRoutes、pages、resultPages、runPanels、headerActions、cleanup、ingress、assistantQueries、workflowValidators、templates、migrations；声明与注册必须一致，重复键、缺依赖、循环和错误 Schema 明确拒绝。金融 provider/strategy 属于 plugin/contracts/trading。

后端导出 Register(sdk.Registrar,sdk.Host) error。Host 提供 Store/Stores、Network、Events、Inbox、OutboundProxy、Realtime，以及 Platform/Sessions/Credentials。Platform 是有界平台/工作流/目录查询、插件只读查询、校验及创建停用草稿；没有发布/激活/运行。插件不导入 Core 内部服务或读取 Core 业务表，按拥有层使用 Host。

## 节点与副作用

Config/Input/Output 使用 JSON Schema Draft 2020-12，UISchema 为对象；输出为合法 JSON 对象，branch 符合 Branches。Core 不解释业务 ready/summary；静态配置放 Config，动态值来自 InputBindings。Secret 用 x-coinsphere-secret 声明，通过 SecretReader 读取，值不进入图、日志、前端 props 或报告。

StatePersistent 成功状态与检查点同事务提交，命名空间包含 workflow/revision/node，状态图串行。ArtifactStore 使用内容寻址。HTTP/AI 声明 external；数据、通知与人工操作使用实际副作用分类。仅 operation key/事务/对账可证明安全重试时使用 RetrySafe；Idempotency-Key 本身不证明外部幂等。sdk.ExecutionError 分类 permanent/transient/unknown_result，未知结果先核对，诊断只复用检查点。

Trigger 响应取消，通过 Emitter 发布事件。Loop 内真实节点引用提供 RewriteConfigNodeIDs；WorkflowValidator 拥有领域约束，逐 K 线计算归 Quant。Cleanup 在 Core 给定同一事务清理所属数据，Ingress 验证领域协议，状态拥有层记录日志。

## Scope 与页面

| Scope | 处理规则 |
| --- | --- |
| SystemScope | 个人有效会话/已加载插件；使用 Core 注入的 WorkflowIDs/AllWorkflows，长连接 SessionValid |
| WorkflowScope | 锁定 WorkflowID/RevisionID/NodeInstanceID/PluginID，不接受扩大归属参数 |
| ResultScope | 所属页面当前请求的 Resources/Scope/Filters/AllowedActions，没有 viewId 或分享授权 |

ResultPage.Resources 提取工作流/节点引用，ValidateScope 使用给定 tx 校验领域归属。ScopeSchema/FilterSchema 验证请求；Actions 与 RouteDescriptor.Action 必须匹配。结果写入在领域事务内调用 Revalidate，再校验状态与幂等；系统写操作通过 Host.Sessions.ValidateSession(ctx,tx)。不接受客户端 userId 替换身份。

普通/结果页都进入导航页面目录。空库首次初始化统一挂在“插件”一级目录下；PluginMenuDescriptor 保留插件声明，后续布局由用户管理，插件加载不能修改用户菜单。后续新增页面待用户挂载。稳定地址 /plugins/{id}/{page|result}/{key}，用户负责父级、标题、图标、排序及隐藏。菜单不建立生命周期引用，卸载时保留布局。

## Vue 贡献

| 导出 | key 与 props |
| --- | --- |
| pages | PageDescriptor.pageKey，普通页面 |
| headerActions | HeaderActionDescriptor.key，最小顶栏组件（AI chat） |
| nodeEditors | NodeDescriptor.EditorKey；config/schema/uiSchema，emit update(key,value) |
| runPanels | panelKey；result:{run,runNode,outputSchema?}；可复用前端 SDK schemaOutputFields 展示中文输出 |
| resultPages | pageKey；context:{pluginId,pageKey,allowedActions}，页面自行管理筛选、查询、详情和导出 |
| providerConfigComponents | 显式 provider key，调用方提供 pluginId |

值为 () => import('./Component.vue')，按明确归属加载，缺插件/key/编译组件须显示错误，不遍历试加载。使用 frontend/src/components/workflow 通用 Schema 表单；业务页面保持 ArtDesignPro 主题、表格、弹窗和可访问交互，不从插件深层导入 Core views。

AI 节点选择 modelId，模型管理/助手 API 和数据均归 official.ai/plugin_ai；调用开始配置快照，密钥掩码；显式模型内网端点独立于 Connector 公网限制。

## migration 与生命周期

Core 与插件分别拥有 schema_migrations_g5。官方基线独立，外部插件最新版本在生成目录记录；启动只读验证账本。migration 连接绑定 schema/PG session lock，不改共享连接池、不遗留 search_path。正式 Paper 观察后已有 SQL 冻结，仅追加。

升级版本递增、旧 SQL 字节不变，当前 draft/published 定义及在途 Run 活动引用解除后才升级/卸载；菜单不阻断。跨 major 不提供兼容层。安装失败恢复源码、表、依赖和新增 migration；Down 被保护拒绝时使用配对恢复点。卸载保留 schema/历史引用；purge-data 要求全部引用解除、已卸载和精确确认。

CLI 与维护步骤见[开发](runbooks/development.md)，空库与恢复见[数据库](runbooks/database-migrations.md)。默认远端 CI 验证注册/依赖、Scope/事务、生命周期、示例 Go/Vue 与实际领域行为；外部调用替身，金融 Decimal/UTC。合成用例不替代源码审查和现场恢复。
