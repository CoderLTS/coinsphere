# 发布、候选初始化与回退

## 拓扑与准备

生产为单实例 Go backend，镜像内置 Vue/ArtDesignPro 产物。PostgreSQL 16 和 dpanel_stack 属于服务器共享服务；CoinSphere 只拥有独立数据库/用户与 data/backend 文件目录。默认项目和容器名均为 coinsphere、数据库 coinsphere_g5 和端口 8081。容器名跟随项目名，生产 Nginx upstream 为 `coinsphere:6987`。普通升级保持身份；再次清空重建按[数据库手册](database-migrations.md#已部署-g5-再次重建)执行，不能当成普通 Up。

Runner 需要 Docker Compose v2、jq、curl 和本机 Registry。先按[数据库手册](database-migrations.md)完成旧资源清单、恢复点、新数据库/用户和独占目录；新 runtime.env 与 .env 权限 0600，凭据仅留服务器。首次部署从 COINSPHERE_DATABASE_PASSWORD_FILE 指定的 0600 普通文件读取预建用户密码；设置 COINSPHERE_STACK_ROOT 时默认读取 secrets/coinsphere-g5-database-password。密码不进入 GitHub Secrets、命令行或日志。

发布工作流只接受最新 main 和未使用版本标签。服务器 Runner 环境可指定 COINSPHERE_DEPLOY_DIR、COINSPHERE_COMPOSE_PROJECT、COINSPHERE_DATABASE_NAME、COINSPHERE_DATABASE_USER、COINSPHERE_WEB_PORT；未设置时从服务器配置的 COINSPHERE_STACK_ROOT 使用 compose/coinsphere 独立目录。主机路径与凭据保留在服务器，不配置进 Actions。普通升级不能更改项目或数据库身份。候选通过独立端口验收。Nginx upstream 必须使用唯一服务地址；共网的多个 Compose backend 会共享 `backend` 别名，不能用该别名区分旧/新实例。

## 发布与验收

1. 所属改动完成；远端 CI 全绿及主 Agent 最终只读复审通过。合并按用户当前授权处理，Ready PR 必须以 main 为 base。
2. 选定维护窗口，保存旧部署清单与一致恢复点；新配置由用户提供，不导入旧定义、模型、代理、凭据、历史或备份。
3. 从最新 main 手工运行 Release and deploy，输入未使用 vX.Y.Z。工作流构建、扫描并推送应用镜像，Manifest 绑定 commit/version/backend digest。
4. 部署器只拉取 backend，执行目标镜像 coinsphere-migrate Up，在候选空库初始化 g5；启动 backend --no-deps，检查容器健康与 /health 后保存 .env。无业务工作流自动创建或启用。
5. 用户确认实际登录、菜单编辑后重启持久性、插件目录、顶栏助手、安全功能和必要实际外部服务；合成测试不当作生产验收。
6. 验收后切换 Nginx upstream，建立新恢复点，再按精确清单清理旧专属容器、数据库/用户、目录、配置与备份，共享 PostgreSQL/网络/其他服务保留。

直接部署已扫描 Manifest 使用同一脚本：

```bash
bash deploy/production/deploy.sh vX.Y.Z /path/to/release-manifest.json
```

4GB Runner 保持单并行 BuildKit、2304m 内存、3072m 内存加 Swap 总上限、前端 Node 1792m 堆和 Go 单包编译；镜像直接推送本机 Registry，固定 digest 部署。构建缓存结束后限 2gb，保留既有 Registry retention。工作流不创建 GitHub Release，也不自动清理旧部署。

## 仅更改 Compose 项目名称

项目更名需要用户授权并安排短暂维护窗口；部署器仍拒绝普通升级改绑身份。此操作复用当前已扫描的镜像 digest、数据库、凭据和文件，不执行初始化或 migration。

1. 从现有 backend 标签确认项目、部署目录、镜像和挂载；核对目标项目、容器名和目录没有冲突，记录其他容器状态。将 Compose、`.env`、runtime 配置、Nginx 配置和 Runner 环境的恢复副本保存在服务器。
2. 只停止旧项目的 backend，将独占目录移动到 compose/coinsphere，更新 `.env` 中的 `COINSPHERE_COMPOSE_PROJECT=coinsphere` 和最新生产 Compose；数据库身份与镜像 digest 保持原值。同步 Runner 的部署目录和项目覆盖值。
3. 使用新项目执行 `up -d --no-deps --wait backend`。容器名由项目名生成，生产为 `coinsphere`；Nginx upstream 改为 `coinsphere:6987`，通过 `nginx -t` 后 reload。
4. 确认容器 ready、源站和公网可用、账号和菜单数据保留、其他容器没有变化，再删除已停止的旧 backend 和无连接的旧专属 default 网络。不得执行项目级 Down 或操作共享网络、数据库及其他服务。
5. 失败时只停止/移除新 backend，将目录和配置恢复后定向启动旧 backend，再恢复 Nginx 和 Runner 配置。验收完成前保留恢复副本；后续正常升级使用新项目。

## GitHub 仓库与 Runner

仓库为 `CoderLTS/coinsphere`，默认分支为 `main`。CI 可以手工运行；生产工作流仍需单独授权触发。`production` 环境只允许 `main` 部署，生产 Runner 保留 `coinsphere-release`、`production` 标签。

仓库重建后必须重新注册 Runner、恢复服务器上的部署环境配置，并验证 Runner 在线和 CI；仅改 Git remote 不会迁移 Runner 注册。旧克隆应重新克隆，避免把旧分支或标签推回新仓库。仓库初始化不会重置生产数据库或修改当前运行镜像。

## 失败恢复

- Manifest/扫描失败不停止运行实例；初始化错误 generation 拒绝启动。
- 首次候选失败只停止/移除该项目的 backend，数据目录和数据库保留；原部署独立存在，入口可保持或切回。
- g5 后续升级失败恢复上一 Compose/.env/digest，仅之前在运行的 backend 才重新启动；不执行 Down，不清理共享服务或数据库/文件。
- schema 不兼容时使用配对恢复点，禁止旧镜像指向 g5 库，禁止伪造版本或删除账务事实。
- 外部投递可能已发生，恢复前核对幂等/投递事实，不自动重放。

用户验收前旧恢复资料保留；清理后旧状态不可恢复。正式 Paper 观察按[Paper 手册](paper-recovery.md)另行记录，部署成功不代表开始观察。当前 Binance 只包含公共数据和 Paper，没有真实交易或私有账户协议。
