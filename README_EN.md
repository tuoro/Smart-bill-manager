# Smart Bill Manager

> **中文：** [README.md](README.md)

Smart Bill Manager is a self-hosted AI workspace for financial documents. It turns payment screenshots, invoices, and trip material into traceable candidates; a candidate becomes formal financial data only after explicit human review and confirmation.

> [!IMPORTANT]
> `v0.3.1` is a public-testing prerelease of the Clean Slate system. The first distributable image supports single-host `linux/amd64` only. Formal real-model evaluation, real mailbox integration, TLS/domain setup, and production deployment are not complete.

## Quick deployment

You need Git, Docker Engine, Docker Compose 2.24.4 or newer, and at least 6 GiB of available memory.

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
- there is no legacy upgrade, import, or compatibility path.

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
