# Docker 单机自托管部署

本指南用于在一台 `linux/amd64` 主机上部署 Smart Bill Manager 公开实测版。默认只监听 `127.0.0.1:8080`，适合在部署主机本机实测。它不是带 TLS、域名、远程数据库或高可用能力的生产部署方案。

## 前置条件

- `linux/amd64`；
- Docker Engine 和 Docker Compose 2.24.4 或更新版本（发布 overlay 使用官方 [`!reset` 合并语义](https://docs.docker.com/reference/compose-file/merge/#reset-value)）；
- 一条命令安装还需要 `curl`、`sha256sum` 和 `tar`；
- 至少 6 GiB 可用内存和足够的数据库、对象文件空间；
- 首次拉取镜像时能访问 `ghcr.io` 和 Docker Hub。

首次进入 Clean Slate 新架构只支持全新数据库和对象目录，不读取或迁移 `v0.2.4` 及更早版本数据。完成首次安装后，后续新架构版本默认保留当前 PostgreSQL 数据并执行版本化结构升级。

## 1. 获取固定部署包

从所选 GitHub Release 下载同版本的以下两个附件：

- `smart-bill-manager-docker-v0.3.4.tar.gz`；
- `smart-bill-manager-docker-v0.3.4.tar.gz.sha256`。

在同一目录校验并展开：

```bash
sha256sum -c smart-bill-manager-docker-v0.3.4.tar.gz.sha256
tar -xzf smart-bill-manager-docker-v0.3.4.tar.gz
cd smart-bill-manager-docker
```

部署包只包含当前 Compose、版本镜像清单、部署工具和必要文档，不包含源码、凭据或运行数据。也可以使用固定源码 Tag：

```bash
git clone https://github.com/tuoro/Smart-bill-manager.git
cd Smart-bill-manager
git checkout v0.3.4
```

使用源码 Tag 时不要运行根目录遗留的 `docker-compose.yml` 或 `Dockerfile`；它们属于旧系统。新系统只通过 `tools/sbm-deploy.sh` 编排 `infra/compose/` 下的当前契约。

## 2. 一条命令或引导式安装

可从固定 Tag 流式取得安装器；安装器随后下载同版本 Bundle 和 sidecar，在本地验证 SHA-256 后才执行 Bundle 内入口：

```bash
version=v0.3.4; curl -fsSL --proto '=https' --tlsv1.2 "https://raw.githubusercontent.com/tuoro/Smart-bill-manager/${version}/tools/install-self-hosted.sh" | sh -s -- --release-version "$version"
```

部署包根目录也提供安装器：

```bash
./install.sh
```

固定源码 Tag 使用：

```bash
./tools/install-self-hosted.sh
```

安装器依次询问运行目录、PostgreSQL 数据目录、附件对象目录、备份目录、Owner 登录信息和本机 HTTP 端口。直接回车采用默认值；例如可把三类持久化目录分别设置为独立数据盘下尚不存在的子目录。路径必须是绝对路径，父目录必须已存在，三个目标不能相同；安装器不会覆盖或接管已有目录。

配置完成后，安装器创建 owner-only secret，暂停提示保存一次性 Owner 密码，再按固定顺序完成镜像拉取、PostgreSQL provision、Schema migration、Owner bootstrap、应用启动和状态检查。PostgreSQL 与应用始终是两个独立容器，数据库不发布宿主端口。

引导式安装完成后可直接跳到“日常操作”。

## 3. 手工创建仓库外运行目录

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

准备器会创建一份主密钥、三份独立 PostgreSQL 角色密码、一份一次性 Owner 密码，以及只含非秘密配置和 secret 文件路径的 `deployment.env`。目录权限为 `0700`，文件为 `0600`，secret 值不会打印。

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
├── owner-password     # 初始化成功后自动删除
└── deployment.env
```

`data/postgres`、`data/objects`、主密钥和认证备份共同构成恢复边界，不能只复制其中一个目录。不要手工编辑 PostgreSQL 数据文件，也不要从对象目录单独删除文件。

初始化前请从 `$runtime_directory/owner-password` 把 Owner 密码录入密码管理器；初始化成功后部署工具会删除该一次性文件。主密钥和三个数据库密码必须持续保留并独立备份，丢失后无法恢复现有数据或 Provider 密文。

## 4. 拉取固定镜像

```bash
./tools/sbm-deploy.sh "$runtime_directory" pull
```

部署配置固定 Smart Bill Manager 和 PostgreSQL 17 的内容摘要，不使用 `latest`。当前应用镜像为 `linux/amd64`；其他架构会明确失败，不做模拟或自动替换。

## 5. 初始化数据库结构和唯一 Owner

以下示例使用测试身份，请按需替换显示名称、租户名称、币种和 IANA 时区：

```bash
./tools/sbm-deploy.sh "$runtime_directory" bootstrap \
  owner@example.invalid \
  "Owner" \
  "My Workspace" \
  CNY \
  Asia/Shanghai
```

Compose 会自动部署内部 PostgreSQL 17，普通用户无需填写数据库地址、账户或端口，也不需要手工运行 SQL。该命令依次等待 PostgreSQL 健康、创建最小权限角色、在空数据库执行 Clean Slate `0001` 结构初始化、创建唯一 Owner，并在成功后删除一次性 Owner 密码文件。命令失败时不要反复重试；先按终端中的稳定错误定位根因。

## 6. 启动并登录

```bash
./tools/sbm-deploy.sh "$runtime_directory" start
./tools/sbm-deploy.sh "$runtime_directory" status
```

浏览器打开 <http://127.0.0.1:8080>，使用初始化邮箱和已记录的 Owner 密码登录。登录后在“AI 配置”页面创建配置，依次完成能力检测和激活；API Key 只通过页面提交并加密保存，不要写入 `deployment.env`、Compose 或命令行。

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

### `docker run` 风格

README 中的 `docker run -d` 示例是已初始化部署的“应用容器启动等价式”，不是完整安装器。它要求 Compose 已创建 PostgreSQL 容器、`smart-bill-manager_database` internal 网络、最小权限角色、Schema、Owner 和受保护 secret 文件；应用启动后还需连接默认 bridge，才能调用用户后续配置的 Provider。

切换前先用 `./tools/sbm-deploy.sh "$runtime_directory" stop` 停止 Compose app，避免容器和端口冲突。直接容器必须保留 README 示例中的只读根、受限 tmpfs、capability、PID、CPU、内存和停止窗口；主密钥与运行数据库密码仍使用只读文件挂载，不能改成 `-e` 明文环境变量。

直接 Docker CLI 不会获得 Compose 的声明式 lifecycle、依赖顺序和升级编排，因此首次安装、升级和恢复仍以一键安装器与 Compose wrapper 为权威入口。不得为了把命令压缩成单个容器而把 PostgreSQL、管理员密码或 migration 权限放进应用容器。

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
