# Docker 单机自托管部署

本指南用于在一台 `linux/amd64` 主机上部署 Smart Bill Manager 公开实测版。默认只监听 `127.0.0.1:8080`，适合在部署主机本机实测。它不是带 TLS、域名、远程数据库或高可用能力的生产部署方案。

## 前置条件

- `linux/amd64`；
- Docker Engine 和 Docker Compose 2.24.4 或更新版本（发布 overlay 使用官方 [`!reset` 合并语义](https://docs.docker.com/reference/compose-file/merge/#reset-value)）；
- 一条命令安装还需要 `curl`、`sha256sum` 和 `tar`；
- 至少 6 GiB 可用内存和足够的数据库、对象文件空间；
- 首次拉取镜像时能访问 `ghcr.io` 和 Docker Hub。

首次进入 Clean Slate 新架构只支持全新数据库和对象目录，不读取或迁移 `v0.2.4` 及更早版本数据。完成首次安装后，后续新架构版本默认保留当前 PostgreSQL 数据并执行版本化结构升级。

## 1. 一条命令安装（推荐）

安装器从固定 Tag 流式取得，随后下载同版本 Bundle 和 sidecar，在本地验证 SHA-256 后才执行 Bundle 内入口：

```bash
curl -fsSL --proto '=https' --tlsv1.2 \
  https://raw.githubusercontent.com/tuoro/Smart-bill-manager/v0.4.0/tools/install-self-hosted.sh \
  | sh -s -- --release-version v0.4.0
```

安装器依次询问运行目录、PostgreSQL 数据目录、附件对象目录、备份目录和本机 HTTP 端口。直接回车采用默认值；例如可把三类持久化目录分别设置为独立数据盘下尚不存在的子目录。路径必须是绝对路径，父目录必须已存在，三个目标不能相同；安装器不会覆盖或接管已有目录。

配置完成后，安装器创建 owner-only secret，再按固定顺序完成镜像拉取、PostgreSQL provision、Schema migration、应用启动和状态检查。PostgreSQL 与应用始终是两个独立容器，数据库不发布宿主端口。

完成后打开 <http://127.0.0.1:8080>（或安装时填写的端口）。页面会引导你创建 Owner 账号——安装器不再询问 Owner 信息，也不再生成需要你手工抄录的一次性密码。安装到此结束，后续参见“首次登录后”和“日常操作”。

## 2. 离线部署包或固定源码 Tag 安装

无法从主机直接流式取得安装器时，先获取部署包再运行其中的安装器。从所选 GitHub Release 下载同版本的两个附件，在同一目录校验并展开：

```bash
sha256sum -c smart-bill-manager-docker-v0.4.0.tar.gz.sha256
tar -xzf smart-bill-manager-docker-v0.4.0.tar.gz
cd smart-bill-manager-docker
./install.sh
```

部署包只包含当前 Compose、版本镜像清单、部署工具和必要文档，不包含源码、凭据或运行数据。

也可以使用固定源码 Tag：

```bash
git clone https://github.com/tuoro/Smart-bill-manager.git
cd Smart-bill-manager
git checkout v0.4.0
./tools/install-self-hosted.sh
```

使用源码 Tag 时不要运行根目录遗留的 `docker-compose.yml` 或 `Dockerfile`；它们属于旧系统。新系统只通过 `tools/sbm-deploy.sh` 编排 `infra/compose/` 下的当前契约。

两种方式进入的引导流程与第 1 步完全相同。

## 3. 手工分步安装（高级）

只有需要逐步检查或脚本化每一步时才使用本节；第 1、2 步的安装器已经按相同顺序执行下列全部命令。

### 3.1 创建仓库外运行目录

下面的示例把运行材料放在仓库同级目录。目标目录必须是绝对路径、尚不存在且位于 Git 仓库外。

```bash
mkdir -p ../sbm-runtime-parent
runtime_directory="$(realpath ../sbm-runtime-parent)/deployment"
./tools/prepare-self-hosted-deployment.sh "$runtime_directory"
```

手工流程同样支持自定义目录和端口：

```bash
./tools/prepare-self-hosted-deployment.sh "$runtime_directory" \
  --postgres-directory /absolute/new/postgres \
  --objects-directory /absolute/new/objects \
  --backups-directory /absolute/new/backups \
  --http-port 7476
```

准备器会创建一份主密钥、三份独立 PostgreSQL 角色密码，以及只含非秘密配置和 secret 文件路径的 `deployment.env`。目录权限为 `0700`，文件为 `0600`，secret 值不会打印。

新安装的完整持久化布局如下：

```text
deployment/
├── data/
│   ├── postgres/      # PostgreSQL 17 数据目录
│   └── objects/       # 原始上传与规范化对象
├── backups/           # 认证备份包目标
├── master-key
├── postgres-admin-password
├── postgres-migration-password
├── postgres-runtime-password
└── deployment.env
```

`data/postgres`、`data/objects`、主密钥和认证备份共同构成恢复边界，不能只复制其中一个目录。不要手工编辑 PostgreSQL 数据文件，也不要从对象目录单独删除文件。

主密钥和三个数据库密码必须持续保留并独立备份，丢失后无法恢复现有数据或 Provider 密文。

### 3.2 拉取固定镜像

```bash
./tools/sbm-deploy.sh "$runtime_directory" pull
```

部署配置固定 Smart Bill Manager 和 PostgreSQL 17 的内容摘要，不使用 `latest`。当前应用镜像为 `linux/amd64`；其他架构会明确失败，不做模拟或自动替换。

### 3.3 初始化数据库结构

```bash
./tools/sbm-deploy.sh "$runtime_directory" bootstrap
```

Compose 会自动部署内部 PostgreSQL 17，普通用户无需填写数据库地址、账户或端口，也不需要手工运行 SQL。该命令依次等待 PostgreSQL 健康、创建最小权限角色，并在空数据库执行 Clean Slate `0001` 结构初始化。Owner 不在这一步创建——应用启动后在浏览器完成。命令失败时不要反复重试；先按终端中的稳定错误定位根因。

### 3.4 启动并登录

```bash
./tools/sbm-deploy.sh "$runtime_directory" start
./tools/sbm-deploy.sh "$runtime_directory" status
```

浏览器打开 <http://127.0.0.1:8080>，按页面提示创建 Owner 账号，随后登录。

## 4. 纯 Docker CLI 部署（不使用 Compose）

不想引入 Compose 时，用两条 `docker run` 起 PostgreSQL 和应用即可。应用容器发现数据库还没有 Schema 时会自行完成初始化，Owner 则在浏览器里创建，不需要单独执行 provision、migrate 和 bootstrap-owner。

本节走的是**单角色**模式：应用使用的数据库账号同时具备建表权限，`sbm_admin` / `sbm_migration` / `sbm_runtime` 三层权限分离在这条路径上不成立。应用被攻破时攻击者可以直接修改表结构。需要权限分离时使用第 1、2 步的安装器，或按第 3 步分步执行。

同时它没有 Compose 的依赖顺序、健康等待和升级编排，升级和恢复仍以安装器与 `sbm-deploy.sh` 为权威入口。

### 4.1 网络与数据库

网络名和容器名随意，应用那边用 `SBM_POSTGRES_HOST` 指过去即可。唯一要求是使用**自定义网络**——Docker 默认的 `bridge` 网络不提供按容器名解析。

```bash
docker network create my-net

docker run -d --name my-postgres --network my-net \
  --restart unless-stopped \
  -e POSTGRES_USER=sbm_app \
  -e POSTGRES_DB=smart_bill_manager \
  -e POSTGRES_PASSWORD=<数据库密码> \
  -v sbm-postgres:/var/lib/postgresql/data \
  postgres:17-alpine
```

数据库不需要发布宿主端口。

### 4.2 应用

```bash
docker run -d --name smart-bill-manager --network my-net \
  --restart unless-stopped --init --stop-timeout 20 \
  -p 127.0.0.1:8080:8080 \
  -e SBM_POSTGRES_HOST=my-postgres \
  -e SBM_POSTGRES_USER=sbm_app \
  -e SBM_POSTGRES_PASSWORD=<数据库密码> \
  -v sbm-data:/var/lib/sbm \
  ghcr.io/tuoro/smart-bill-manager:v0.4.0
```

打开 <http://127.0.0.1:8080>，页面会引导你创建 Owner 账号并填写工作区名称、币种和时区，创建完成后即可登录。用户自定义网络自带出站访问，Provider 调用无需再执行 `docker network connect`。

### 4.2.1 使用已有的 PostgreSQL

数据库连接的四项都是普通环境变量，指向任意可达实例即可——同一台机器上已有的 Postgres、NAS 上的共用实例或另一台主机。此时不需要 4.1，也不需要自定义网络：

```bash
  -e SBM_POSTGRES_HOST=192.168.1.10 \
  -e SBM_POSTGRES_PORT=5432 \
  -e SBM_POSTGRES_DATABASE=smart_bill_manager \
  -e SBM_POSTGRES_USER=sbm_app \
  -e SBM_POSTGRES_PASSWORD=<数据库密码> \
```

该账号需要能在目标库建表（首启要应用迁移）。跨主机连接应把 `SBM_POSTGRES_SSL_MODE` 设为 `verify-full` 并通过 `SBM_POSTGRES_ROOT_CERTIFICATE_FILE` 挂载根证书；默认的 `disable` 只适合同机或可信内网。

### 4.3 参数说明

**`SBM_DEPLOYMENT_MODE`（默认 `local`，通常不用传）。** 声明这个部署跑在明文回环还是 TLS 之后。

- `local`：会话 Cookie 不带 `Secure` 标志，不发送 HSTS 头。**明文 HTTP 必须用这个**——`Secure` Cookie 浏览器只在 HTTPS 下回传，在 `http://127.0.0.1` 上设了就会登录不上。
- `production`：Cookie 自动带 `Secure`，并发送 `Strict-Transport-Security`。

镜像默认 `local`，本节的明文回环部署不需要传。`SBM_COOKIE_SECURE` 同样不用传，默认由模式推导。只有一种情况需要显式覆盖：`local` 模式但由外部反向代理终止 TLS，此时设 `SBM_COOKIE_SECURE=true`。`production` 模式下设成 `false` 会被拒绝启动。

Compose 路径不受影响：[compose.yaml](../infra/compose/compose.yaml) 仍强制显式声明这两项，安装器生成的 `deployment.env` 也照旧写入。

**其余有默认值，按需覆盖。** `SBM_POSTGRES_HOST`（`database`）、`SBM_POSTGRES_PORT`（`5432`）、`SBM_POSTGRES_DATABASE`（`smart_bill_manager`）、`SBM_POSTGRES_USER`（`sbm_runtime`）、`SBM_POSTGRES_SSL_MODE`（`disable`）、`SBM_SESSION_TTL`（`168h`）、`SBM_AI_CONCURRENCY`（`2`）。objects 路径、poppler 路径和迁移目录由镜像固定，正常不需要改。

**数据库密码。** `SBM_POSTGRES_PASSWORD` 是明文回退，仅在未挂载 `/run/secrets/sbm_postgres_runtime_password` 时生效；两者并存时文件优先。入口脚本把它写入受限文件后即从环境中移除，应用进程的 `/proc/self/environ` 里不会保留。硬化部署继续使用文件。

**主密钥。** 未挂载 `/run/secrets/sbm_master_key` 时，入口脚本在 `/var/lib/sbm/secrets/master-key` 生成一份并在日志中提示。**必须把它单独备份出去**——丢失后已保存的 Provider API Key 无法恢复。注意它和数据在同一个卷里，这与安装器路径下"主密钥独立托管"的边界不同；备份时需要把它复制到另一处保管。

为避免密钥写进容器可写层后随容器一起丢失，入口脚本要求 `/var/lib/sbm` 或 `/var/lib/sbm/secrets` 确实来自挂载卷，否则以 `master_key_storage_not_persistent` 失败。上面的 `-v sbm-data:/var/lib/sbm` 满足该条件。

**首启初始化。** 应用启动时检查目标库有没有 `schema_migrations`。没有就自行应用全部迁移——此时把运行账号同时当作迁移身份使用，因此该账号需要建表权限，这就是本节开头所说的单角色模式。已有 Schema 则完全不触发：Compose 路径下 app 总在独立的 `migrate` 入口之后启动，硬化路径因此不受影响。

**创建 Owner。** 没有任何环境变量参与，全部在浏览器完成。首次访问任意页面都会被引导到一次性初始化页 `/setup`，填写邮箱、姓名、工作区名称、币种、时区和密码即可。创建成功后 `GET /api/v1/setup` 永久返回 `required: false`，该页面不再出现，重复提交被拒绝。

"只能创建一次"由 [`BootstrapOwner`](../apps/api/internal/adapters/postgresql/identity.go) 的 Serializable 事务保证——它在同一事务里统计身份记录，非空即回滚，不依赖接口层的预检查，因此并发和重放都无法绕过。

### 4.4 与 Compose 契约的差异

本节有意省略了下面这组加固约束，需要对齐时按 [`infra/compose/compose.yaml`](../infra/compose/compose.yaml) 的 `app` 服务补上：

```bash
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=268435456 \
  --tmpfs /run/sbm-secrets:rw,noexec,nosuid,nodev,size=65536,mode=0700 \
  --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add SETGID --cap-add SETUID \
  --security-opt no-new-privileges:true \
  --pids-limit 256 --cpus 2 --memory 3584m
```

`--read-only` 与两条 `--tmpfs` 必须同时使用：入口脚本要写 `/run/sbm-secrets`，应用要写 `/tmp`。保留 `--cap-drop ALL` 时必须补回那四个 capability，入口脚本要用它们修正 secret 与对象目录属主后再降权到 `sbm`。

其余差异：数据库角色不做权限分离；网络非 internal，数据库容器也具备出站访问；升级只是替换镜像 tag 重建应用容器，迁移在首启时自动应用，但没有 `upgrade --backup-confirmed` 的备份门禁——升级前请自行按[备份与恢复说明](backup-restore.md)完成备份。

## 首次登录后

登录后在“AI 配置”页面创建配置，依次完成能力检测和激活；API Key 只通过页面提交并加密保存，不要写入 `deployment.env`、Compose 或命令行。

真实模型正确率尚未完成正式评测。实测时应使用清晰、完整、无遮挡且关键字段可直接辨读的原始图片，并始终人工审核 Claim 后再确认 Fact。

## 日常操作

```bash
./tools/sbm-deploy.sh "$runtime_directory" status
./tools/sbm-deploy.sh "$runtime_directory" logs
./tools/sbm-deploy.sh "$runtime_directory" stop
./tools/sbm-deploy.sh "$runtime_directory" down
./tools/sbm-deploy.sh "$runtime_directory" start
```

`down` 只移除容器和网络，保留数据库、对象和配置目录。部署工具故意不提供删除持久数据的命令；不要使用 `docker compose down --volumes`、`docker volume rm` 或手工删除对象文件。

### 直接使用 Docker Compose

`sbm-deploy.sh` 是对唯一 Compose 契约的顺序封装。完成首次 bootstrap 后，也可以显式调用同一组文件：

```bash
docker compose --project-name smart-bill-manager \
  --env-file "$runtime_directory/deployment.env" \
  --env-file infra/compose/release.env \
  -f infra/compose/compose.yaml \
  -f infra/compose/compose.release.yaml \
  up -d --no-build --pull never --wait app
```

不要在全新数据库上直接执行该命令来替代安装器；首次安装还需要按顺序执行 database health、provision、migration 和 Owner bootstrap。

## 新架构版本升级

Clean Slate 是旧架构进入当前系统时的一次性边界，不是每次升级都清库。当前 PostgreSQL 数据在后续版本中默认保留，数据库变更由连续且事务化的 Schema migration 完成。

升级前必须停止外部写入，并按备份说明创建和独立验证当前版本的认证备份。然后把新部署包展开到新目录，继续使用原来的绝对 `runtime_directory`：

```bash
./tools/sbm-deploy.sh "$runtime_directory" pull
./tools/sbm-deploy.sh "$runtime_directory" upgrade --backup-confirmed
```

升级命令会停止 app、保持 PostgreSQL 数据和对象不变、刷新最小权限角色、顺序应用尚未执行的 migration，再启动并等待 app 就绪。未提供 `--backup-confirmed` 时命令拒绝执行；migration 失败时 app 保持停止，不能通过清库、修改 migration 记录或旧镜像直读新 Schema 绕过。

已有 `v0.3.1` 部署若使用 Docker named volume，会继续使用原卷；新部署工具不会自动移动或删除它。是否把旧 named volume 转为宿主目录必须通过独立认证备份与全新目标恢复完成，不能直接复制 PostgreSQL 数据目录。

## 备份、升级与恢复

- 升级或维护前先停止写入，并按 [备份与恢复说明](backup-restore.md) 创建和独立验证认证备份；
- 数据库、对象卷、主密钥和认证备份必须分别托管；
- `v0.3.x` 不支持旧系统数据导入，但当前 Clean Slate PostgreSQL 数据在后续版本中默认保留；
- 当前没有自动更新器。新版本必须先阅读 Release 说明、完成备份，再显式更新代码和镜像 digest；
- 恢复只允许写入全新目标，不能覆盖现有数据库或对象目录。

更深入的容量、健康检查、入口错误分类和恢复边界见 [本地运维说明](local-operations.md)。

## 网络暴露边界

默认 `local` 模式只适用于回环 HTTP。不要把 `SBM_BIND_ADDRESS` 改为 `0.0.0.0` 后直接暴露到局域网或公网。域名、TLS、反向代理、Secure Cookie、远程 PostgreSQL、高可用和生产部署尚未完成正式门禁，需要独立方案与验收。
