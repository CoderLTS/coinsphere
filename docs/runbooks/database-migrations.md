# generation 5 初始化与恢复

CoinSphere 只支持 PostgreSQL 16。当前空库基线为 generation 5：Core 使用 public.schema_migrations_g5，官方 AI、Quant、Binance、Notification 使用各自 plugin_* schema 的 schema_migrations_g5；外部插件仍有独立 schema/账本。旧 public.schema_migrations 或 schema_migrations_g4 明确拒绝初始化，不原地升级，不转换或导入旧数据。

## 基线与启动

Core SQL 位于 backend/internal/migration/generation5；插件 SQL 位于 backend/plugin/official/{ai,quant,binance,notification}/migrations。coinsphere-migrate Up 显式建立基线，可重复执行；启动只读验证 generation、PostgreSQL 主版本及 Core/已编译插件的账本。缺版本、超前/落后或错误 generation 均拒绝启动，不手工修改账本绕过。

g5 基线 Down 明确拒绝；回退通过配对镜像、数据库、配置/加密 key 与独占文件恢复点完成。尚未正式 Paper 观察时可以重建基线；正式观察开始时冻结既有 SQL，此后只追加版本。

## 基线整理边界

正式 Paper 观察前，用户明确授权重建时可以直接整理 generation 5 的单版本初始化 SQL；这不代表会修改已初始化数据库。Core 与各官方插件仍分别从版本 1 建立独立账本，不增加旧数据转换或启动自动 DDL。已有库不会因 SQL 文件重写而自动获得新约束；本次基线重建必须配合空库初始化，不得将旧账本标记为新基线已完成。

菜单翻译只服务菜单，`biz_type` 仅允许 `menu`，`biz_id` 外键引用菜单并随菜单删除；不再接受已删除的按钮权限配置。初始化回归覆盖孤立翻译拒绝及级联删除。运行历史、金融 Decimal、幂等和事务约束继续保留。

恢复使用本次重建前保存的配对镜像、数据库、配置和文件，不执行基线 Down；正式 Paper 观察开始后 SQL 冻结，后续仅追加版本。

## 维护窗口

1. 记录旧 CoinSphere 专属容器、数据库/用户、文件目录、配置和备份的精确清单，以及镜像 digest、commit、数据库版本。仅记录标识和路径，秘密值留在服务器。
2. 保存旧部署用于失败回退；停止旧实例的写入者后，建立数据库/文件/配置/镜像配对恢复点。共享 PostgreSQL、网络和其他服务保持原状。
3. 创建新的独立 PostgreSQL 16 空数据库与最小授权用户，创建新的 Compose 项目及独占文件目录，不复制旧 runtime.env、数据库或制品。
4. 从新配置初始化唯一账号与默认菜单。新认证 key/密码和模型、代理、插件凭据由用户在受限服务器环境重新提供；不向 AI/CI 展示。
5. 固定 digest 部署候选，确认健康、登录、账号唯一性、节点/页面/插件目录，以及未自动建立或启用业务工作流。修改导航后重启，验证布局保持；用户验收实际界面与外部服务。
6. 候选失败则停候选 backend，入口切回原部署；不把旧镜像指向 g5 数据库。候选 schema/文件保留供调查，不操作共享服务。
7. 用户验收通过后切换正式入口，按清单删除旧专属容器、数据库/用户、目录、配置及旧备份。删除数据库前重新核对独占关系，路径必须是精确已验收清单；禁止项目级 down、批量容器清理或共享目录递归删除。

删除旧恢复资料后不能恢复旧状态，工作流、凭据与历史需重新配置或产生。清理是维护窗口的最终步骤，部署器不自动清理。

## 已部署 g5 再次重建

只有用户明确要求清空时执行。先完成基线独立 PR、集成 PR、远端 CI 和最终只读复审，再进入维护窗口；不通过重跑 Up、删表或手改账本来伪装空库初始化。

1. 从容器标签和受限配置核对当前 Compose 项目、数据库/用户、绑定目录、固定 digest 及入口；记录共享服务边界。停止 CoinSphere 的所有写入者，保存配对数据库 dump、文件、配置与镜像恢复点。
2. 在原数据库精确名称下重新创建空库，保留该应用的最小授权用户；其他数据库和角色不变。移开旧上传、静态和制品目录，创建同属主的新空目录，不导入旧配置项、工作流、模型、代理或历史。
3. 在服务器生成新的认证 key 与初始密码，写入 0600 配置；数据库连接凭据留在服务器。登录信息保存到部署操作账号的受限文件，由用户自行读取；其他账号的 `~` 不是部署账号主目录。
4. 用已扫描的新 digest 执行初始化，再启动唯一 backend。校验新的 database_id、generation、独立账本、唯一账号、默认菜单、六个官方插件，以及空工作流/模型/代理；检查重启持久性、会话撤销和公网健康。
5. 失败时先停止新实例，用配对旧镜像/数据库/文件/配置恢复；不能把旧镜像直接连到新基线。成功后建立新恢复点，清理本次已授权删除的旧库/目录和恢复资料，保留共享服务。

## 命令与证据

在目标服务器使用配对配置与镜像：

```bash
/app/coinsphere-migrate -config /server/g5.yml -direction status
/app/coinsphere-migrate -config /server/g5.yml -direction up
/app/coinsphere-migrate -config /server/g5.yml -direction version
```

COINSPHERE_DATABASE__DSN、认证 key 等仅由权限受限配置注入，不在命令行或日志展开。远端 CI 覆盖空库/重复初始化、单账号约束、错误 generation、核心独立运行、菜单持久性与插件账本。生产恢复与清理证据来自现场清单、用户验收和维护记录；合成 CI 不代表现场完成。
