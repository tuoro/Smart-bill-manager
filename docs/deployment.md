# v0.3.1 单机自托管部署

本指南用于在一台 `linux/amd64` 主机上部署 Smart Bill Manager `v0.3.1` 公开实测预发布版。默认只监听 `127.0.0.1:8080`，适合在部署主机本机实测。它不是带 TLS、域名、远程数据库或高可用能力的生产部署方案。

## 前置条件

- `linux/amd64`；
- Git；
- Docker Engine 和 Docker Compose 2.24.4 或更新版本（发布 overlay 使用官方 [`!reset` 合并语义](https://docs.docker.com/reference/compose-file/merge/#reset-value)）；
- 至少 6 GiB 可用内存和足够的数据库、对象文件空间；
- 首次拉取镜像时能访问 `ghcr.io` 和 Docker Hub。

本版本只支持全新数据库和对象卷，不读取或迁移 `v0.2.4` 及更早版本数据。

## 1. 获取固定版本

```bash
git clone https://github.com/tuoro/Smart-bill-manager.git
cd Smart-bill-manager
git checkout v0.3.1
```

不要使用仓库根目录遗留的 `docker-compose.yml` 或 `Dockerfile`；它们属于旧系统。新系统只通过 `tools/sbm-deploy.sh` 编排 `infra/compose/` 下的当前契约。

## 2. 创建仓库外运行目录

下面的示例把运行材料放在仓库同级目录。目标目录必须是绝对路径、尚不存在且位于 Git 仓库外。

```bash
mkdir -p ../sbm-runtime-parent
runtime_directory="$(realpath ../sbm-runtime-parent)/deployment"
./tools/prepare-self-hosted-deployment.sh "$runtime_directory"
```

准备器会创建一份主密钥、三份独立 PostgreSQL 角色密码、一份一次性 Owner 密码，以及只含非秘密配置和 secret 文件路径的 `deployment.env`。目录权限为 `0700`，文件为 `0600`，secret 值不会打印。

初始化前请从 `$runtime_directory/owner-password` 把 Owner 密码录入密码管理器；初始化成功后部署工具会删除该一次性文件。主密钥和三个数据库密码必须持续保留并独立备份，丢失后无法恢复现有数据或 Provider 密文。

## 3. 拉取固定镜像

```bash
./tools/sbm-deploy.sh "$runtime_directory" pull
```

部署配置固定 Smart Bill Manager 和 PostgreSQL 17 的内容摘要，不使用 `latest`。当前应用镜像为 `linux/amd64`；其他架构会明确失败，不做模拟或自动替换。

## 4. 初始化唯一 Owner

以下示例使用测试身份，请按需替换显示名称、租户名称、币种和 IANA 时区：

```bash
./tools/sbm-deploy.sh "$runtime_directory" bootstrap \
  owner@example.invalid \
  "Owner" \
  "My Workspace" \
  CNY \
  Asia/Shanghai
```

该命令依次等待 PostgreSQL 健康、创建最小权限角色、执行 Clean Slate `0001`、创建唯一 Owner，并在成功后删除一次性 Owner 密码文件。命令失败时不要反复重试；先按终端中的稳定错误定位根因。

## 5. 启动并登录

```bash
./tools/sbm-deploy.sh "$runtime_directory" start
./tools/sbm-deploy.sh "$runtime_directory" status
```

浏览器打开 <http://127.0.0.1:8080>，使用初始化邮箱和已记录的 Owner 密码登录。登录后在“AI Provider”页面创建配置，依次完成能力检测和激活；API Key 只通过页面提交并加密保存，不要写入 `deployment.env`、Compose 或命令行。

真实模型正确率尚未完成正式评测。实测时应使用清晰、完整、无遮挡且关键字段可直接辨读的原始图片，并始终人工审核 Claim 后再确认 Fact。

## 日常操作

```bash
./tools/sbm-deploy.sh "$runtime_directory" status
./tools/sbm-deploy.sh "$runtime_directory" logs
./tools/sbm-deploy.sh "$runtime_directory" stop
./tools/sbm-deploy.sh "$runtime_directory" down
./tools/sbm-deploy.sh "$runtime_directory" start
```

`down` 只移除容器和网络，保留数据库与对象卷。部署工具故意不提供删除卷命令；不要使用 `docker compose down --volumes`、`docker volume rm` 或手工删除对象文件。

## 备份、升级与恢复

- 升级或维护前先停止写入，并按 [备份与恢复说明](backup-restore.md) 创建和独立验证认证备份；
- 数据库、对象卷、主密钥和认证备份必须分别托管；
- `v0.3.x` 不支持旧系统数据导入；
- 当前没有自动更新器。新版本必须先阅读 Release 说明、完成备份，再显式更新代码和镜像 digest；
- 恢复只允许写入全新目标，不能覆盖现有数据库或对象目录。

更深入的容量、健康检查、入口错误分类和恢复边界见 [本地运维说明](local-operations.md)。

## 网络暴露边界

默认 `local` 模式只适用于回环 HTTP。不要把 `SBM_BIND_ADDRESS` 改为 `0.0.0.0` 后直接暴露到局域网或公网。域名、TLS、反向代理、Secure Cookie、远程 PostgreSQL、高可用和生产部署尚未完成正式门禁，需要独立方案与验收。
