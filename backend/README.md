# CoinSphere Go 后端

Core 1.0.0 / SDK 1 / Graph 3 的个人工作流微内核：唯一账号、菜单、生命周期、定义/发布、调度/事件、运行、审批、日志、状态/检查点、制品、个人收件箱和代理。可信官方/源码插件拥有业务功能；AI 模型/助手属于 plugin_ai，Binance 只公共行情和 Paper。

只支持 PostgreSQL 16，显式 coinsphere-migrate Up 初始化 g5 空库，启动只读验证 schema。Core public 和 AI/Quant/Binance/Notification 专属 schema 分别拥有 schema_migrations_g5；旧账本拒绝原地 Up，基线 Down 拒绝。初次 seed 写 coinsphere 账号及一次导航，不覆盖已存在密码或布局。

```powershell
# 仅在用户要求的隔离开发环境或发布维护环境运行。
go run ./cmd/migrate -config ./config.yml -direction up
go run ./cmd/migrate -config ./config.yml -direction status
go run ./cmd/coinsphere plugin validate <可信插件目录>
```

插件 install/upgrade/uninstall/purge-data 继续沿用当前 CLI；卸载保留数据/布局，purge 要求已卸载、所有历史引用解除与精确确认。默认不在本地运行测试/构建/应用/数据库演练，质量门禁交远端 CI。

| 目录 | 职责 |
| --- | --- |
| main.go、internal/api | 组装与 /api/v1 输入/身份/Scope 边界 |
| internal/service、db | Core 事务/运行/导航与模型 |
| internal/migration/generation5 | 空库基线与版本保护 |
| workflow/graph、plugin/sdk | 通用图与可信插件公开契约 |
| plugin/official | AI、Connector、Notification、QQ、Quant、Binance |
| internal/pluginbuild、pluginregistry、pluginlifecycle | 静态注册、引用保护及失败安装恢复 |
| version/builtin.json | 官方版本/依赖/贡献唯一目录 |

详细说明见[代码结构](../docs/code-structure.md)、[插件开发](../docs/plugin-development.md)、[公共契约](../docs/contracts/README.md)、[数据库初始化](../docs/runbooks/database-migrations.md)。秘密值禁止进入日志、图、CI、PR 或 AI 上下文。
