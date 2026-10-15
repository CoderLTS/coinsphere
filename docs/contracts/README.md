# 公共契约

本契约对应 Core 1.0.0、SDK major 1、Graph schemaVersion 3 和 PostgreSQL 16。generation 5 使用空库初始化，不提供旧 API、旧图、旧数据转换或迁移。初始化与恢复见[数据库手册](../runbooks/database-migrations.md)。

## HTTP 与身份

成功响应为 `{code:200,msg:"",data:...}`，错误为 RFC 9457 Problem Details，包含 type/title/status/detail/requestId。ID 是真实数据库 ID；UTC 时间使用 RFC 3339，金融值使用十进制字符串。

除登录、健康、静态资源与凭据验证的 Ingress 外，API 使用 Bearer access token。仅接受唯一 coinsphere 账号的有效会话；JWT 必须含匹配数据库 session_version 的正整数 sv，旧会话无 sv 时拒绝。登出持久撤销当前 tokenId；写操作在事务中重新检查会话。菜单仅用于导航。/me 不返回角色或用户授权字段；用户、角色、权限、工作流及结果分享管理 API 已删除，返回 404。

| 接口                                                       | 语义                                                                                            |
| ---------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| POST /api/v1/workflows                                     | graph/template、metadata、secretChanges 和运行配置原子创建；返回真实工作流与初始 draft 指针     |
| GET /api/v1/workflows                                      | 分页摘要，含草稿/发布修订号、最近运行状态及触发时间；不要求前端逐行取详情                     |
| POST /api/v1/workflows/{id}/revisions                      | expectedDraftRevisionId、graph、可选 metadata/secretChanges 原子保存；过期指针返回冲突          |
| POST /api/v1/workflows/{id}/publish                        | revisionId、expectedPublishedRevisionId 显式发布；必要凭据或插件声明不满足时拒绝                |
| POST /api/v1/workflows/{id}/lifecycle                      | activate/deactivate 等生命周期操作；自动触发绑定发布定义                                        |
| POST /api/v1/workflows/{id}/runs                           | revisionId、entryPoint、input 创建固定快照运行；手工试运行不切换发布指针                        |
| GET /api/v1/workflows/{id}/runs、GET /api/v1/workflow-runs | 工作流运行或跨工作流运行记录，游标分页并支持工作流/状态/触发方式/时间/关键词筛选                |
| GET/POST /api/v1/workflow-runs/{runId}                     | 查询真实 Run 与节点尝试；有效会话下的 cancel/retry/replay                                       |
| GET /api/v1/human-tasks、POST /api/v1/human-tasks/{taskId} | 人工待办游标分页，可按工作流、运行和状态筛选；一次性 approve/reject，已决定/过期返回冲突        |
| PUT /api/v1/me/password                                    | currentPassword、newPassword；校验当前密码，保存新哈希并递增 session_version，全部会话立即失效  |
| /api/v1/plugins/{pluginId}/results/...                     | 插件声明 ResultPageKey 的直接结果路由；按 ScopeSchema/FilterSchema 解析当前请求，无固定视图写入 |
| GET /api/v1/plugins/catalog                                | 已加载插件的结果页和运行面板目录；节点另由 /workflows/node-definitions 提供                     |

所有受保护请求继续校验资源存在性、运行状态、当前结果请求范围及插件作用域；制品必须有可访问的引用，不能仅凭 SHA256 读取。站内通知统一发送给个人账号，节点不接受站内用户或角色收件人配置。

## Graph 3

`POST /api/v1/workflows/validate` 返回 `{valid, issues[]}`。问题包含 `scope`、`level`、稳定的机器 `code` 和中文 `message`；可定位时补充 `nodeInstanceId`、`edgeId` 与相对于对应对象的 `fieldPath`。字段路径保留原始键名，循环子图对象使用父节点编号加 `.` 前缀。校验沿用首个失败即返回的流程，界面不得将缺少定位信息的问题伪装为字段错误。节点目录通过 `pluginId`/`pluginName` 表示插件归属，通过 `category`/`categoryTitle` 分离机器分类与中文显示名称。

图由 schemaVersion、entryPoints、nodes、edges 组成。entryPoints 使用通用名字，main 是默认入口；每个入口指向有效节点，不要求 realtime/backtest 固定名称。节点保留 nodeInstanceId/nodeType/nodeVersion/config/inputBindings/position，版本精确校验。节点与连线的可选 label 是最多 80 字符的显示名称，不参与执行；保存完整往返，不改写历史修订。画布固定左右端口、直角折线和圆角连接，线上仅显示简短名称，CEL 全文在连线属性面板。

Core 的 core.condition 1.0.0 采用 match=all/any 与 rules（fieldPath/operator/value），输入使用当前 inputBindings。输出 branch=true/false/unavailable、available 与 matched。全部规则有效评估后组合；缺失、null、类型不符走 unavailable，matched=null；数值比较使用 Decimal。普通运行与 Quant 帧共享纯计算实现。

| Binding.kind | 取值规则                                          |
| ------------ | ------------------------------------------------- |
| literal      | value 是完整 JSON 值，Decimal 必须为字符串        |
| field        | nodeInstanceId 与 fieldPath 从指定节点输出取值    |
| input        | fieldPath 从入口输入取值                          |
| cel          | expression 显式访问 input、nodes、event、incoming |

CEL 示例：`nodes["check"].ready && input.amount == "1.25"`。输出按节点 ID 命名空间访问。incoming 只包含到达该节点的实际已选边及来源输出；边 condition、来源端口和声明 branch 共同决定可达性。未选分支跳过，汇合节点执行一次，不对 ready/triggered 做隐式判断。

Config 只存静态配置；Secret 字段以 ConfigSchema 的 x-coinsphere-secret 声明，正文通过 secretChanges 独立提交，不进入图。修订查询只返回 secretFields 是否已配置。替换、保留或移除凭据随保存事务完成；发布和运行校验 required Secret。

Loop config 包含 maxIterations、timeoutSeconds、exitCondition 和 body。循环体用 core.loop_item/core.loop_end，展开节点 ID 为父ID.子ID；退出表达式中的 input 是当前 iteration/value。循环体不能持久等待，父节点不占子节点槽，绝对期限沿用最早 attempt。

## Run、错误与状态

Run 状态原样返回 queued/running/waiting/retrying/succeeded/failed/cancelled；等待与重试有独立语义。RunNode 保留 attempt/loopIteration/invocationStarted 和受控摘要。修订与检查点不可变，被 Run 引用的旧修订不会阻断正常保存。

租约 token、有效期限与授权保护开始调用、状态、检查点、日志及终态写入。永久错误不重试，transient 在节点声明安全重试时最多三次，unknown_result 阻断自动/人工直接重试。HTTP/AI 调用按外部副作用处理；对端 Idempotency-Key 本身不证明幂等。诊断复用原副作用检查点，缺证据失败。

审批创建与 waiting 同事务；决定、取消、过期和替代原子恢复 Run，不会产生已决定任务仍永久 waiting。持久状态按 workflow/revision/node 隔离，含状态图全工作流串行提交。

## SDK 1 与贡献

ActionRequest 提供 Revision、NodeInstanceID、OperationKey、Input、Config、Secrets、State、Artifacts、Incoming、GraphSnapshot 与 Logger；ActionResult 提供 Output/Artifacts。TriggerRequest 与 Emitter 用于持久事件，Trigger 必须响应取消。金融类型和逐 K 线计算归插件领域契约。

PluginDescriptor 声明 contributes，不声明角色或权限。manifest 保持 schemaVersion 1、sdkMajor 1；官方版本与真实依赖来自 backend/version/builtin.json。注册拒绝重复插件/节点/贡献、依赖缺失及错误 Schema。ResultPage.Actions 和 RouteDescriptor.Action 保留页面动作匹配，不能以任意动作扩大请求资源。

| Scope | 路由与约束 |
| --- | --- |
| SystemScope | /api/v1/plugins/{pluginId}/*；有效个人会话、已安装且已加载插件；WorkflowIDs/AllWorkflows 由 Core 注入 |
| WorkflowScope | /api/v1/workflows/{workflowId}/nodes/{nodeInstanceId}/plugins/{pluginId}/*；锁定修订、节点归属及插件 |
| ResultScope | /api/v1/plugins/{pluginId}/results/*；所属 ResultPageKey 的当前请求 Resources/Scope/Filters/AllowedActions，不存在固定视图 ID |

结果查询只接受 ScopeSchema/FilterSchema 声明的单值字段；Resources 提取工作流/节点引用，ValidateScope 验证领域归属。结果写入必须在业务事务内调用 ResultScope.Revalidate，并复验业务状态与幂等。System 长连接调用 SessionValid，其他写操作通过 Host.Sessions 在事务内复验。制品仍必须存在可访问的引用。插件卸载后对应路由返回 404。

Host 提供 Platform、Sessions、Credentials、Store/Stores、Network、OutboundProxy、Inbox、Realtime 和 Events。Platform 只提供有界平台摘要、工作流查询、节点/插件目录、插件只读查询、草稿校验和创建停用草稿；不暴露发布、激活或运行。AI 不能直接依赖 Core 内部实现或业务表。

Vue pages、resultPages、runPanels、nodeEditors 和最小 headerActions 按 pluginId/贡献类型/key 加载。普通页面和结果页面接收 `PluginPageContext { pluginId, pageKey, allowedActions }`，页面自行维护 scope、filters、URL 状态、查询、详情、导出和页面内交互；宿主只负责稳定路由、目录校验、组件加载和加载失败提示。RunPanel 接收 result:{run,runNode,outputSchema?}；outputSchema 为当前节点的输出 Schema，插件可复用 SDK 的 schemaOutputFields 按中文元数据展示，原始 JSON 保持原值。缺插件或组件显示明确错误。

## 导航

菜单页面引用为 pluginId + contributionType(page/result) + pageKey，数据库保证每页最多一个入口。目录可多级嵌套，不能形成环，也不能把页面作为父级。用户选择页面后系统生成稳定 name/path/component；页面地址为 /plugins/{pluginId}/{page|result}/{pageKey}，移动菜单不改地址。

用户可调整父级、标题、图标、排序、隐藏与停用状态。POST/PUT /system/menus 保存后，前端刷新路由、导航、面包屑和标签。GET /system/menus/pages 返回可绑定页面目录及 menuId（0 表示未挂载）。默认账号和布局只初始化一次，后续重启、升级、插件重载不覆盖布局；新页面保持未挂载。插件缺失时入口隐藏、管理树保留，直接访问显示错误。菜单不建立插件活动引用，卸载不修改用户布局。

## AI 插件

模型 CRUD 位于 /api/v1/plugins/official.ai/models，助手位于同命名空间的 /assistant。数据属于 plugin_ai；原核心模型/助手 API 不存在。model_call 节点只保存 modelId，调用开始读取启用模型的端点、密钥、模型名称与超时快照，配置修改作用于后续调用。删除/停用模型返回明确错误。请求与结构化响应有界，密钥加密存储、API 只返回固定掩码。

模型端点允许用户明确配置的 HTTP/HTTPS 公网及内网地址，拒绝 userinfo/query/fragment 和交易所域名，不跟随重定向。Connector 的公网精确域名白名单与私网限制独立保留。AI 的 chat 顶栏贡献仅在插件加载时展示；助手会话 SSE 定期复验身份，最终写入与草稿创建在事务中复验。助手只创建未发布且停用的草稿，后续发布、激活和运行由用户操作。

## 生命周期与数据库

安装、编译、加载三个事实分开，安装版本与编译版本不相等则不加载。必需依赖与重复贡献注册明确失败。现行 draft/published 与在途 Run 维护活动引用；建立引用与升级/卸载锁同一安装行。升级只允许版本递增且旧 migration 字节不变，并要求先解除全部活动引用；跨 major 不提供兼容假象。

卸载保留数据和历史引用；purge-data 要求已卸载、所有引用解除、匹配确认文本。失败安装/升级恢复源码、注册表、依赖文件和新增 migration，回滚本身失败需人工恢复点处理。插件是可信同进程代码，Scope 是契约边界，不是恶意代码隔离。
Core 使用 public.schema_migrations_g5；官方 AI、Quant、Binance、Notification 与外部插件各有专属 schema 和 g5 账本。应用启动只读验证 PostgreSQL 16、generation 5 和匹配版本，初始化由 coinsphere-migrate 完成。Binance 只注册公共行情、Paper 和导出，不包含 Live、私有账户流、私有协议或账户放行。
