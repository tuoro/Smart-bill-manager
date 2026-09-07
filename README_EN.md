# Smart Bill Manager

> **中文：** [README.md](README.md)

Smart Bill Manager is a self-hosted AI workspace for financial documents. It turns payment screenshots, invoices, and trip material into traceable candidates; a candidate becomes formal financial data only after explicit human review and confirmation.

> [!IMPORTANT]
> `v0.4.0` is a public-testing prerelease of the Clean Slate system. The distributable image supports single-host `linux/amd64` only. Formal real-model evaluation, real mailbox integration, TLS/domain setup, and production deployment are not complete.

## Installation

Requires a `linux/amd64` host, Docker Engine, and at least 6 GiB of available memory.

Create a user-defined network first — Docker's default `bridge` network does not resolve container names, and the app finds the database by name — then start the two containers:

```bash
docker network create my-net

docker run -d --name smart-bill-manager-db --network my-net \
  --restart unless-stopped \
  -e POSTGRES_USER=sbm_app \
  -e POSTGRES_DB=smart_bill_manager \
  -e POSTGRES_PASSWORD=<choose a database password> \
  -v sbm-postgres:/var/lib/postgresql/data \
  postgres:17-alpine

docker run -d --name smart-bill-manager --network my-net \
  --restart unless-stopped --init --stop-timeout 20 \
  -p 127.0.0.1:8080:8080 \
  -v sbm-data:/var/lib/sbm \
  ghcr.io/tuoro/smart-bill-manager:v0.4.0
```

Open <http://127.0.0.1:8080>. The page guides you through two steps: the database host, port and name are already prefilled to match the commands above, so only the account and password are needed ("test connection" is available); once it verifies, the schema is created. Then create the administrator account with a username and password.

The connection can also be pinned with `-e SBM_POSTGRES_HOST`, `-e SBM_POSTGRES_USER` and `-e SBM_POSTGRES_PASSWORD`, which skips the first step. If you already run PostgreSQL, skip the first container and point at it instead.

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
