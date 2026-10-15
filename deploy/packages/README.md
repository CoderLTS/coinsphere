# CoinSphere 二进制发布包

包内包含后端服务 `coinsphere-server`、基线迁移 `coinsphere-migrate`、插件管理 `coinsphere`、默认配置、前端静态文件和 Nginx 配置。Windows 可执行文件使用 `.exe` 后缀。

这不是桌面应用。启动后端前必须设置安全的 `COINSPHERE_AUTH__SECRET_KEY` 等环境变量；Go 服务会直接托管同目录下的 `web` 前端产物。需要 TLS 或域名入口时，可按随包 `nginx.conf` 让 Nginx 反向代理 API、WebSocket 和健康检查到后端 `6987` 端口。

Windows 包的后端目标架构为 `GOOS=windows GOARCH=386`；Linux 包为 `GOOS=linux GOARCH=amd64`。

启动后端前必须在独立空 PostgreSQL 16 数据库建立 generation 5 基线，不导入旧数据；步骤见仓库 `docs/runbooks/database-migrations.md`。插件 CLI 的源码安装和 registry 生成需要完整源码工作区及 Go/Vue 工具链，二进制目录不能直接安装新源码插件。Binance 仅支持公共行情与 Paper，不包含真实交易接口。
