# CoinSphere 生产 Compose

模板运行单个内置 Vue 产物的 Go backend，通过外部 dpanel_stack 网络连接现有 PostgreSQL 16。生产默认项目和容器名均为 coinsphere、数据库/用户 coinsphere_g5、端口 127.0.0.1:8081。容器名跟随项目名，候选项目使用独立名称。目录必须独占，不复制旧配置或数据。

- compose.yaml：backend 和现有共享数据库网络。
- deploy.sh：固定 digest、初始化、健康检查及 backend 定向失败恢复，不清理旧部署。
- runtime.env.example：运行 Secret 模板，不含数据库 DSN。
- data/backend：上传、静态文件和制品的独占绑定目录。

首次部署前在服务器预建独立空数据库/用户，将相同密码存入 0600 普通文件，通过服务器环境 COINSPHERE_DATABASE_PASSWORD_FILE 指定。脚本保存 0600 的 .env 和 runtime.env，后续部署复用数据库身份、密码、端口和 data；已有项目/库/用户不能改绑。旧部署目录没有 g5 身份记录时拒绝复用。

```bash
# 仅在服务器配置，文件内容不在终端回显。
export COINSPHERE_DEPLOY_DIR=/path/to/coinsphere
export COINSPHERE_DATABASE_PASSWORD_FILE=/path/to/protected/database-password
export COINSPHERE_COMPOSE_PROJECT=coinsphere
export COINSPHERE_DATABASE_NAME=coinsphere_g5
export COINSPHERE_DATABASE_USER=coinsphere_g5
export COINSPHERE_WEB_PORT=8081
bash deploy/production/deploy.sh vX.Y.Z release-manifest.json
```

目录已有 runtime.env，或指定 COINSPHERE_RUNTIME_ENV_FILE；设置 COINSPHERE_STACK_ROOT 时默认目录为 compose/coinsphere，Secret 来源为 secrets/coinsphere-g5-runtime.env 和 secrets/coinsphere-g5-database-password。主机路径与凭据不配置进 Actions。脚本不自动发现旧项目，不创建/删除共享数据库或网络。

COINSPHERE_WORKFLOW__HTTP_ALLOWED_HOSTS 继续控制 Connector 精确公网域名；默认 [] 拒绝全部目标。AI 模型端点由用户单独明确配置，可为内网 HTTP/HTTPS，不受 Connector 白名单限制，保留 URL、超时、响应上限及禁止重定向。

部署 Up 显式初始化 g5；应用只校验 schema。失败只 stop/rm backend 并恢复已有 g5 的 Compose/.env，之前运行的 backend 才恢复启动；不 Down、不操作其他服务、不清数据库/文件。生产维护窗口、入口切换和验收后清理见[发布手册](../../docs/runbooks/release.md)及[数据库手册](../../docs/runbooks/database-migrations.md)。

已部署 g5 的普通升级保持部署身份；仅项目更名按[发布手册](../../docs/runbooks/release.md#仅更改-compose-项目名称)定向切换。再次初始化必须走数据库 Runbook 的显式清空流程，部署器不会自动删库。生产 Nginx 使用唯一 upstream `coinsphere:6987`，避免多个项目共享 backend 别名时串流量。
