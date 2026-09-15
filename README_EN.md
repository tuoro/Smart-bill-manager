# Smart Bill Manager

> **中文：** [README.md](README.md)

Smart Bill Manager is a self-hosted AI workspace for financial documents. It turns payment screenshots, invoices, and trip material into traceable candidates; a candidate becomes formal financial data only after explicit human review and confirmation.

> [!IMPORTANT]
> `v0.6.0` is a public-testing prerelease of the Clean Slate system. The distributable image supports single-host `linux/amd64` only. Formal real-model evaluation, real mailbox integration, TLS/domain setup, and production deployment are not complete.

## Installation

Requires a `linux/amd64` host, Docker Engine, and at least 6 GiB of available memory. Pick either path.

### With docker run

Start the two containers. No extra network is needed:

```bash
docker run -d --name smart-bill-manager-db \
  --restart unless-stopped \
  -p 127.0.0.1:5432:5432 \
  -e POSTGRES_USER=sbm_app \
  -e POSTGRES_DB=smart_bill_manager \
  -e POSTGRES_PASSWORD=<choose a database password> \
  -v sbm-postgres:/var/lib/postgresql/data \
  postgres:17-alpine

docker run -d --name smart-bill-manager \
  --restart unless-stopped --init --stop-timeout 20 \
  -p 127.0.0.1:8080:8080 \
  -v sbm-data:/var/lib/sbm \
  ghcr.io/tuoro/smart-bill-manager:v0.6.0
```

Then read the database container's IP address, which the next step asks for:

```bash
docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' smart-bill-manager-db
```

Open <http://127.0.0.1:8080>. The page guides you through two steps. First the database connection: replace the host with the IP address printed above, keep the prefilled port `5432` and database name `smart_bill_manager`, use the account `sbm_app` and the password you chose ("test connection" is available). Once it verifies, the schema is created. Then create the administrator account with a username and password.

The `-p 127.0.0.1:5432:5432` on the database exposes it on the host loopback so that `psql`, backup tools or a GUI client can reach it. It binds `127.0.0.1` only, so it is not reachable from the local network. Pick another host port if 5432 is taken, for example `-p 127.0.0.1:15432:5432`.

The application container does not use that port. Both containers sit on Docker's default `bridge` network and reach each other by IP, so the host field takes the container IP printed above rather than `127.0.0.1`: the default network resolves no container names, and a container cannot reach the host's loopback address.

The connection can also be pinned with `-e SBM_POSTGRES_HOST=<the IP address above>`, `-e SBM_POSTGRES_USER` and `-e SBM_POSTGRES_PASSWORD`, which skips the first step. If you already run PostgreSQL, skip the first container and point at it instead.

Recreating the database container may change its IP address (`docker restart` does not). If it does, the app can no longer connect; an owner enters the new address under System → Database connection and restarts the application container.

### With Docker Compose

Equivalent to the two commands above, flag for flag, written as one file. Download it:

```bash
curl -O https://raw.githubusercontent.com/tuoro/Smart-bill-manager/main/compose.yaml
```

Replace `POSTGRES_PASSWORD` with your own password, leave the rest alone, then start it from the directory holding that file:

```bash
docker compose up -d
```

Open <http://127.0.0.1:8080>. The page guides you through two steps. First the database connection: **the host, port and database name are already prefilled and need no change and no IP lookup** — only fill in the account `sbm_app` and the password you chose. Then create the administrator account.

That is what this path saves over the two `docker run` commands: Compose creates its own network that resolves service names, and the service name matches the container name, which is exactly the address the page prefills.

For the first few seconds the database is still initialising, so "test connection" fails if you click it immediately; wait a moment and click again. The two `docker run` commands behave the same way.

Day-to-day commands run from that directory. Plain `docker logs` and `docker restart` keep working too, because the container names are the same:

```bash
docker compose logs -f smart-bill-manager     # follow the logs
docker compose restart smart-bill-manager     # restart the application
docker compose down                           # stop and remove the containers, keeping the volumes
```

`docker compose down` without `-v` keeps the volumes, so data and the master key survive. To upgrade, change `image` to a newer tag and run `docker compose up -d`; when migrations are pending the app refuses to start, so back up first and then add `SBM_ALLOW_MIGRATION: "true"` to the application service.

The full file is below; you can also create `compose.yaml` yourself and paste this in:

```yaml
# Smart Bill Manager 单机部署，与 README 里那两条 docker run 等价。
#
# 用法：把下面的密码改成自己的，在本文件所在目录执行 docker compose up -d，
# 再打开 http://127.0.0.1:8080 按页面提示配置即可。

name: smart-bill-manager

services:
  smart-bill-manager-db:
    image: postgres:17-alpine
    container_name: smart-bill-manager-db
    restart: unless-stopped
    # 只绑回环，供 psql 和备份工具连接；不要写成 5432:5432，那会把库开给局域网。
    ports:
      - "127.0.0.1:5432:5432"
    environment:
      POSTGRES_USER: sbm_app
      POSTGRES_DB: smart_bill_manager
      POSTGRES_PASSWORD: 改成你自己的数据库密码
    volumes:
      - sbm-postgres:/var/lib/postgresql/data

  smart-bill-manager:
    image: ghcr.io/tuoro/smart-bill-manager:v0.6.0
    container_name: smart-bill-manager
    restart: unless-stopped
    init: true
    stop_grace_period: 20s
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - sbm-data:/var/lib/sbm

volumes:
  sbm-postgres:
  sbm-data:
```

To upgrade, recreate the application container with a newer image tag. When migrations are pending the app refuses to start and says so — migrations rewrite data in place and cannot be rolled back, so create and verify a backup first (see [backup and restore](docs/backup-restore.md)), then recreate with `-e SBM_ALLOW_MIGRATION=true`.

Hardening flags, using an existing PostgreSQL, the volume layout, and day-to-day operations are covered in the [deployment guide](docs/deployment.md); the detailed guide is maintained in Chinese.

## Database and persistence

Everything the application persists lives under `/var/lib/sbm`, so one volume is enough:

```text
/var/lib/sbm/
├── objects/    # uploaded images and PDFs        (sbm:sbm 0700)
├── secrets/    # master key, managed by the entrypoint (root:sbm 0710, traverse only)
└── config/     # database connection and password (sbm:sbm 0700, written by the setup page)
```

PostgreSQL keeps its data in its own container volume. On first start, if no master key is mounted, the entrypoint generates one under `secrets/` and says so in the log — **it sits in the same volume as the data, so copy it somewhere else as well**. Losing it makes stored Provider API keys unrecoverable.

A backup must cover the database, the object files, and the master key, and must be produced as an authenticated backup package (see [backup and restore](docs/backup-restore.md)). Copying the volume or data directory is not a restorable backup.

Clean Slate only means that no legacy schema or SQLite data is read. From this architecture onward, releases keep existing data and upgrade the database through versioned PostgreSQL schema migrations; you are not asked to wipe the database on every update.

## Main capabilities

- image and PDF upload, per-item batch feedback, and multi-page review;
- minimal Chinese multimodal extraction, deterministic local normalization, and field-level validation;
- explicit manual review after extraction failure, full fact search, and traceable corrections preserving Source → Claim → Fact provenance;
- manually created trips combining multiple tickets and other documents, automatic attribution, and supporting materials;
- duplicate candidates, payment-to-invoice allocation, a default 30-day recommendation window, and reasoned cross-period manual allocation;
- bad-debt marking and reversal with related trip deletion protection, without automatic financial write-off;
- local email-attachment archives, reimbursement workflow, and material ZIP exports;
- member invitations, role management, password changes, and local account recovery;
- deterministic insights, tenant isolation, audit, authenticated backup, and complete recovery.

## Security and data boundaries

```text
Source -> Claim -> Fact
original evidence -> reviewable candidate (AI or explicit manual origin) -> user-confirmed data
```

- The model cannot create a Fact directly. Schema validation, deterministic business rules, authorization, and human review are mandatory.
- PostgreSQL 17 is the sole relational data source; money always uses integer minor units.
- API keys are encrypted and the master key is stored separately. Deployment tooling does not place secrets in the environment, command arguments, or repository.
- The new system does not preserve compatibility with legacy code, APIs, databases, or job states, and does not read or migrate data from `v0.2.4` or earlier.
- The default listener is `127.0.0.1`. Do not expose it to a LAN or the Internet without a separately reviewed TLS and production deployment design.

## Current limitations

- the first image supports `linux/amd64` only;
- formal real-model accuracy evaluation is not complete;
- the mailbox UI currently stores credential-free connection descriptors and does not connect to a real mailbox;
- domain, TLS, reverse proxy, remote PostgreSQL, HA, and cloud object storage are not included;
- legacy architecture and SQLite data are not imported; releases within the current Clean Slate PostgreSQL architecture preserve data through schema upgrades.

## Documentation

| Entry | Contents |
| --- | --- |
| [Deployment](docs/deployment.md) | Installation, bootstrap, lifecycle, and network boundary |
| [Local operations](docs/local-operations.md) | Health, capacity, diagnostics, and upgrade boundary |
| [Members and accounts](docs/member-accounts.md) | Invitations, deactivation, password changes, and local recovery |
| [Material exports](docs/material-export.md) | Current trip and fixed reimbursement snapshot ZIPs, file scope, and resource limits |
| [Backup and recovery](docs/backup-restore.md) | Authenticated backup, verification, and complete recovery |
| [Product and scope](docs/product.md) / [Roadmap](docs/roadmap.md) | Positioning, completed scope, and remaining gates |
| [Architecture](docs/architecture.md) / [Data model](docs/data-model.md) | Source, Claim, Fact, and PostgreSQL design |
| [AI pipeline](docs/ai-pipeline.md) | Model contract, normalization, validation, and review |
| [Acceptance](docs/acceptance.md) / [M4 evidence](docs/m4-evidence.md) | Local quality gates and safe aggregate evidence |

Legacy `backend-go/`, `frontend/`, the root Dockerfile, and root Compose files remain historical references only. They are not current runtime entry points. Use the matching historical Release for an older product version.

## Security and license

Report vulnerabilities privately according to [SECURITY.md](SECURITY.md), not in a public Issue. The project is licensed under the [MIT License](LICENSE).
