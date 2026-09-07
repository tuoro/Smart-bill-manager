# Docker 单机自托管部署

本指南用于在一台 `linux/amd64` 主机上部署 Smart Bill Manager 公开实测版。默认只监听 `127.0.0.1:8080`，适合在部署主机本机实测。它不是带 TLS、域名、远程数据库或高可用能力的生产部署方案。

## 前置条件

- `linux/amd64`；
- Docker Engine；
- 至少 6 GiB 可用内存和足够的数据库、对象文件空间；
- 首次拉取镜像时能访问 `ghcr.io` 和 Docker Hub。

首次进入 Clean Slate 新架构只支持全新数据库和对象目录，不读取或迁移 `v0.2.4` 及更早版本数据。完成首次安装后，后续新架构版本默认保留当前 PostgreSQL 数据并执行版本化结构升级。

## 安装

两条 `docker run` 起 PostgreSQL 和应用。应用容器发现数据库还没有 Schema 时会自行完成初始化，数据库连接和管理员账号都在浏览器里配置。

> [!IMPORTANT]
> 这是**单角色**部署：应用使用的数据库账号同时具备建表权限。`sbm_admin` / `sbm_migration` / `sbm_runtime` 三层权限分离不在这条路径上，应用被攻破时攻击者可以直接修改表结构。升级的备份门禁由 `SBM_ALLOW_MIGRATION` 承担，见「参数说明」。

### 1. 网络与数据库

网络名和容器名随意，应用那边用 `SBM_POSTGRES_HOST` 指过去即可。唯一要求是使用**自定义网络**——Docker 默认的 `bridge` 网络不提供按容器名解析。

```bash
docker network create my-net

docker run -d --name smart-bill-manager-db --network my-net \
  --restart unless-stopped \
  -e POSTGRES_USER=sbm_app \
  -e POSTGRES_DB=smart_bill_manager \
  -e POSTGRES_PASSWORD=<数据库密码> \
  -v sbm-postgres:/var/lib/postgresql/data \
  postgres:17-alpine
```

数据库不需要发布宿主端口。

### 2. 应用

```bash
docker run -d --name smart-bill-manager --network my-net \
  --restart unless-stopped --init --stop-timeout 20 \
  -p 127.0.0.1:8080:8080 \
  -v sbm-data:/var/lib/sbm \
  ghcr.io/tuoro/smart-bill-manager:v0.4.0
```

打开 <http://127.0.0.1:8080>，页面分两步引导：先填数据库连接信息（地址已预填为 `smart-bill-manager-db`（即上一步的容器名），账号密码用上一步设置的），验证通过后自动建表；再创建管理员账号。两步都完成后即可登录。

也可以用 `-e SBM_POSTGRES_HOST`、`-e SBM_POSTGRES_USER`、`-e SBM_POSTGRES_PASSWORD` 预先指定，页面就会跳过第一步。环境变量优先于页面写入的配置。用户自定义网络自带出站访问，Provider 调用无需再执行 `docker network connect`。

### 2.1 不建自定义网络（用 IP 对接）

数据库连接只需要地址、端口、账号和密码四项，地址填 IP 完全可以。省掉 `docker network create`：两个容器都留在默认 `bridge` 网络，用 `docker inspect` 取数据库 IP 填进初始化页即可。

```bash
docker run -d --name smart-bill-manager-db \
  -e POSTGRES_USER=sbm_app -e POSTGRES_DB=smart_bill_manager \
  -e POSTGRES_PASSWORD=<数据库密码> \
  -v sbm-postgres:/var/lib/postgresql/data postgres:17-alpine

docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' smart-bill-manager-db
```

默认 bridge 上容器之间按 IP 是互通的，只是**不解析容器名**。

代价是 IP 不稳定：`docker restart` 不变，但**删除重建会变**（实测 `172.22.0.2` → `172.22.0.3`）。数据库容器重建后应用会连不上，需要按下面的方式改配置。用自定义网络加容器名则不受影响，这是默认推荐它的唯一原因。

### 2.2 修改已保存的数据库连接

登录后在「系统 → 数据库连接」里可以查看和修改，只有 Owner 可见。页面提供「检测连接」，保存前会先实际连一次，连不上就不保存。

**保存后需要重启应用容器才会生效**——现有连接池和后台任务仍绑定在旧配置上。页面会给出提示：

```bash
docker restart smart-bill-manager
```

连接由环境变量固定时（传了 `SBM_POSTGRES_PASSWORD_FILE` 或 `SBM_POSTGRES_PASSWORD`），页面只展示不可修改，并说明应在容器参数中修改——因为环境变量优先级高于该文件，改文件不会生效。

**数据库已经连不上的情况**下应用无法启动，自然也进不了设置页。此时用环境变量覆盖：`-e SBM_POSTGRES_HOST=<新地址>` 等，重建应用容器即可；或删除 `/var/lib/sbm/config/database.json` 后重启，会重新进入初始化页的数据库配置这一步（已创建的管理员账号不受影响）。应用拒绝启动时的错误信息会给出当前地址、配置文件路径和这两种修复方式。

### 3. 使用已有的 PostgreSQL

数据库连接的四项都是普通环境变量，指向任意可达实例即可——同一台机器上已有的 Postgres、NAS 上的共用实例或另一台主机。此时不需要 4.1，也不需要自定义网络：

```bash
  -e SBM_POSTGRES_HOST=192.168.1.10 \
  -e SBM_POSTGRES_PORT=5432 \
  -e SBM_POSTGRES_DATABASE=smart_bill_manager \
  -e SBM_POSTGRES_USER=sbm_app \
  -e SBM_POSTGRES_PASSWORD=<数据库密码> \
```

该账号需要能在目标库建表（首启要应用迁移）。跨主机连接应把 `SBM_POSTGRES_SSL_MODE` 设为 `verify-full` 并通过 `SBM_POSTGRES_ROOT_CERTIFICATE_FILE` 挂载根证书；默认的 `disable` 只适合同机或可信内网。

### 4. 参数说明

**`SBM_DEPLOYMENT_MODE`（默认 `local`，通常不用传）。** 声明这个部署跑在明文回环还是 TLS 之后。

- `local`：会话 Cookie 不带 `Secure` 标志，不发送 HSTS 头。**明文 HTTP 必须用这个**——`Secure` Cookie 浏览器只在 HTTPS 下回传，在 `http://127.0.0.1` 上设了就会登录不上。
- `production`：Cookie 自动带 `Secure`，并发送 `Strict-Transport-Security`。

镜像默认 `local`，本节的明文回环部署不需要传。`SBM_COOKIE_SECURE` 同样不用传，默认由模式推导。只有一种情况需要显式覆盖：`local` 模式但由外部反向代理终止 TLS，此时设 `SBM_COOKIE_SECURE=true`。`production` 模式下设成 `false` 会被拒绝启动。

发布验证使用的内部 Compose 配置仍强制显式声明这两项，不受镜像默认值影响。

**其余有默认值，按需覆盖。** `SBM_POSTGRES_HOST`（`database`）、`SBM_POSTGRES_PORT`（`5432`）、`SBM_POSTGRES_DATABASE`（`smart_bill_manager`）、`SBM_POSTGRES_USER`（`sbm_runtime`）、`SBM_POSTGRES_SSL_MODE`（`disable`）、`SBM_SESSION_TTL`（`168h`）、`SBM_AI_CONCURRENCY`（`2`）。objects 路径、poppler 路径和迁移目录由镜像固定，正常不需要改。

**数据库连接。** 三种来源，按优先级：`SBM_POSTGRES_PASSWORD_FILE`（挂载 secret 文件）> `SBM_POSTGRES_PASSWORD` 明文环境变量 > 初始化页写入的 `/var/lib/sbm/config/database.json`。明文环境变量由入口脚本写入受限文件后即从环境中移除，应用进程的 `/proc/self/environ` 里不会保留。

初始化页写入的配置同样只保存非秘密字段；密码单独存为同目录下的 0600 文件，与挂载 secret 走同一条读取契约。连接验证失败时两个文件都会被删除，部署回到未配置状态而不是固化一份连不上的设置。

**卷内布局。** `-v sbm-data:/var/lib/sbm` 下有三个目录，权限各不相同：`objects/`（`sbm:sbm 0700`，上传的原件）、`secrets/`（`root:sbm 0710`，只放主密钥，应用只能穿越不能写）、`config/`（`sbm:sbm 0700`，初始化页写入的数据库配置与密码）。主密钥与应用可写目录刻意分开，避免被攻破的应用覆盖它。

**主密钥。** 未挂载 `/run/secrets/sbm_master_key` 时，入口脚本在 `/var/lib/sbm/secrets/master-key` 生成一份并在日志中提示。**必须把它单独备份出去**——丢失后已保存的 Provider API Key 无法恢复。注意它和数据在同一个卷里，因此备份时必须把它复制到另一处单独保管，不能只依赖卷本身。

为避免密钥写进容器可写层后随容器一起丢失，入口脚本要求 `/var/lib/sbm` 或 `/var/lib/sbm/secrets` 确实来自挂载卷，否则以 `master_key_storage_not_persistent` 失败。上面的 `-v sbm-data:/var/lib/sbm` 满足该条件。

**升级。** 换用新版本镜像重建应用容器即可，但**迁移不会自动执行**。检测到未执行的迁移时应用拒绝启动，并给出待执行条数：

```
检测到 1 条未执行的数据库迁移（已应用 8 / 共 9）。迁移会原地修改现有数据且不可回滚，
请先创建并验证备份，然后设置 SBM_ALLOW_MIGRATION=true 重新启动以应用它们
```

请先按[备份与恢复说明](backup-restore.md)创建并独立验证备份——把数据目录映射到宿主机只提供持久化，不构成可恢复的备份，迁移改坏的数据会被原样保留。确认备份可用后加上 `-e SBM_ALLOW_MIGRATION=true` 重建容器，应用会先执行迁移再启动。迁移完成后可以去掉该变量。

镜像比数据库旧（数据库里有镜像不认识的迁移）时始终拒绝启动，不受该变量影响——旧代码读新结构会写坏数据。

**首启初始化。** 应用启动时检查目标库有没有 `schema_migrations`。没有就自行应用全部迁移——此时把运行账号同时当作迁移身份使用，因此该账号需要建表权限，这就是本节开头所说的单角色模式。已有 Schema 且没有待执行迁移时完全不触发。

**创建管理员。** 没有任何环境变量参与，全部在浏览器完成。首次访问任意页面都会被引导到一次性初始化页 `/setup`，填写管理员用户名和密码即可；姓名、工作区名称、币种和时区在「更多设置」里可选。登录标识符可以是纯用户名（如 `admin`），系统不发送任何邮件，不要求可收信的邮箱地址。创建成功后 `GET /api/v1/setup` 永久返回 `required: false`，该页面不再出现，重复提交被拒绝。

"只能创建一次"由 [`BootstrapOwner`](../apps/api/internal/adapters/postgresql/identity.go) 的 Serializable 事务保证——它在同一事务里统计身份记录，非空即回滚，不依赖接口层的预检查，因此并发和重放都无法绕过。

### 5. 加固参数

上面的命令为了可读性省略了下面这组加固约束。生产以外的本机试用可以不加；需要收紧时补上：

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

`--read-only` 与两条 `--tmpfs` 必须同时使用；保留 `--cap-drop ALL` 时必须补回那四个 capability，入口脚本要用它们修正 secret 与对象目录属主后再降权到 `sbm`。这两点在上文已说明。

## 首次登录后

登录后在“AI 配置”页面创建配置，依次完成能力检测和激活；API Key 只通过页面提交并加密保存，不要写入环境变量或命令行。

真实模型正确率尚未完成正式评测。实测时应使用清晰、完整、无遮挡且关键字段可直接辨读的原始图片，并始终人工审核 Claim 后再确认 Fact。

## 日常操作

```bash
docker logs -f smart-bill-manager       # 查看日志
docker stop smart-bill-manager          # 停止
docker start smart-bill-manager         # 启动
docker restart smart-bill-manager       # 重启
```

停止和删除应用容器不会影响数据：数据库在 PostgreSQL 容器的卷里，对象、主密钥和连接配置在 `/var/lib/sbm` 卷里。不要使用 `docker volume rm` 或 `docker compose down --volumes` 删除这两个卷。

## 升级

换用新的镜像 tag 重建应用容器：

```bash
docker rm -f smart-bill-manager
docker run -d --name smart-bill-manager --network my-net \
  --restart unless-stopped --init --stop-timeout 20 \
  -p 127.0.0.1:8080:8080 \
  -v sbm-data:/var/lib/sbm \
  ghcr.io/tuoro/smart-bill-manager:<新版本>
```

存在未执行的数据库迁移时应用会拒绝启动并给出待执行条数。先按[备份与恢复说明](backup-restore.md)创建并独立验证备份，再加 `-e SBM_ALLOW_MIGRATION=true` 重建；迁移完成后可去掉该变量。详见「参数说明」中的「升级」。

PostgreSQL 容器可以独立升级，但跨大版本需要 `pg_upgrade` 或 dump/restore，不能直接把新版本指向旧数据目录。


## 备份与恢复

- 升级或维护前先停止写入，并按 [备份与恢复说明](backup-restore.md) 创建和独立验证认证备份；
- 数据库、对象文件、主密钥和认证备份必须分别托管。把卷或数据目录整个复制不构成可恢复的备份；
- 当前没有自动更新器。新版本必须先阅读 Release 说明、完成备份，再显式更换镜像 tag；
- 恢复只允许写入全新目标，不能覆盖现有数据库或对象目录。

更深入的容量、健康检查、入口错误分类和恢复边界见 [本地运维说明](local-operations.md)。

## 网络暴露边界

默认 `local` 模式只适用于回环 HTTP。不要把 `SBM_BIND_ADDRESS` 改为 `0.0.0.0` 后直接暴露到局域网或公网。域名、TLS、反向代理、Secure Cookie、远程 PostgreSQL、高可用和生产部署尚未完成正式门禁，需要独立方案与验收。
