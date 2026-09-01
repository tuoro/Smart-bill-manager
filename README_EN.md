# Smart Bill Manager

> **中文：** [README.md](README.md)

Smart Bill Manager is a self-hosted AI workspace for financial documents. It turns payment screenshots, invoices, and trip material into traceable candidates; a candidate becomes formal financial data only after explicit human review and confirmation.

> [!IMPORTANT]
> `v0.3.1` is a public-testing prerelease of the Clean Slate system. The first distributable image supports single-host `linux/amd64` only. Formal real-model evaluation, real mailbox integration, TLS/domain setup, and production deployment are not complete.

## Docker quick deployment

The Release deployment bundle only requires Docker Engine, Docker Compose 2.24.4 or newer, and at least 6 GiB of available memory. Git is only required when deploying from a source tag.

Starting with the next patch release, each GitHub Release also includes `smart-bill-manager-docker-<version>.tar.gz` and its SHA-256 file. The bundle contains only Compose, deployment tools, and required documentation—not the source tree. Download both assets from the selected Release, then run:

```bash
sha256sum -c smart-bill-manager-docker-<version>.tar.gz.sha256
tar -xzf smart-bill-manager-docker-<version>.tar.gz
cd smart-bill-manager-docker
```

The current `v0.3.1` deployment entry point remains available from its fixed source tag:

```bash
git clone https://github.com/tuoro/Smart-bill-manager.git
cd Smart-bill-manager
git checkout v0.3.1

mkdir -p ../sbm-runtime-parent
runtime_directory="$(realpath ../sbm-runtime-parent)/deployment"
./tools/prepare-self-hosted-deployment.sh "$runtime_directory"
./tools/sbm-deploy.sh "$runtime_directory" pull
```

Record the one-time Owner password from `$runtime_directory/owner-password`, then bootstrap and start the application:

```bash
./tools/sbm-deploy.sh "$runtime_directory" bootstrap \
  owner@example.invalid "Owner" "My Workspace" CNY Asia/Shanghai
./tools/sbm-deploy.sh "$runtime_directory" start
```

Open <http://127.0.0.1:8080> and sign in. See the [deployment guide](docs/deployment.md) for prerequisites, security boundaries, daily operations, and backup guidance. The detailed deployment guide is currently maintained in Chinese.

## Database and persistence

The default Compose stack deploys PostgreSQL 17, provisions least-privilege roles, and initializes the database schema automatically. Regular users do not enter a database address or run SQL manually. A new installation keeps all persistent material under its deployment directory:

```text
deployment/
├── data/postgres/     # PostgreSQL data
├── data/objects/      # uploaded images and PDFs
├── backups/           # independently verified backup packages
├── master-key         # master key required for Provider ciphertext
├── postgres-*-password
└── deployment.env     # non-secret runtime settings and secret file paths
```

Back up the database, objects, master key, and authenticated backup set together, while keeping secrets out of Git. `down` removes containers and networks but never these directories.

Clean Slate only rejects legacy architecture and SQLite data. Releases within the current architecture preserve PostgreSQL data by default and apply versioned schema migrations; users are not expected to clear their database for each update.

After creating and independently verifying a backup, update with the new deployment bundle and run:

```bash
./tools/sbm-deploy.sh "$runtime_directory" pull
./tools/sbm-deploy.sh "$runtime_directory" upgrade --backup-confirmed
```

## Main capabilities

- image and PDF upload, per-item batch feedback, and multi-page review;
- minimal Chinese multimodal extraction, deterministic local normalization, and field-level validation;
- Payment, Invoice, and Trip workflows with complete Source → Claim → Fact provenance;
- duplicate candidates, payment-to-invoice allocation, and independent allocation adjustment;
- local email-attachment archives, trip attribution, and reimbursement status workflow;
- deterministic insights, tenant isolation, audit, authenticated backup, and complete recovery.

## Security and data boundaries

```text
Source -> Claim -> Fact
original evidence -> AI candidate -> user-confirmed data
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
| [Backup and recovery](docs/backup-restore.md) | Authenticated backup, verification, and complete recovery |
| [Product and scope](docs/product.md) / [Roadmap](docs/roadmap.md) | Positioning, completed scope, and remaining gates |
| [Architecture](docs/architecture.md) / [Data model](docs/data-model.md) | Source, Claim, Fact, and PostgreSQL design |
| [AI pipeline](docs/ai-pipeline.md) | Model contract, normalization, validation, and review |
| [Acceptance](docs/acceptance.md) / [M4 evidence](docs/m4-evidence.md) | Local quality gates and safe aggregate evidence |

Legacy `backend-go/`, `frontend/`, the root Dockerfile, and root Compose files remain historical references only. They are not current runtime entry points. Use the matching historical Release for an older product version.

## Security and license

Report vulnerabilities privately according to [SECURITY.md](SECURITY.md), not in a public Issue. The project is licensed under the [MIT License](LICENSE).
