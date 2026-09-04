# Smart Bill Manager

> **中文：** [README.md](README.md)

Smart Bill Manager is a self-hosted AI workspace for financial documents. It turns payment screenshots, invoices, and trip material into traceable candidates; a candidate becomes formal financial data only after explicit human review and confirmation.

> [!IMPORTANT]
> `v0.3.4` is a public-testing prerelease of the Clean Slate system. The distributable image supports single-host `linux/amd64` only. Formal real-model evaluation, real mailbox integration, TLS/domain setup, and production deployment are not complete.

## Docker quick deployment

Requires `linux/amd64`, Docker Engine, Docker Compose 2.24.4 or newer, `curl`, `sha256sum`, `tar`, and at least 6 GiB of available memory.

### One-command installation (recommended)

Download the installer from an immutable tag, verify the matching deployment bundle, and enter guided setup:

```bash
version=v0.3.4; curl -fsSL --proto '=https' --tlsv1.2 "https://raw.githubusercontent.com/tuoro/Smart-bill-manager/${version}/tools/install-self-hosted.sh" | sh -s -- --release-version "$version"
```

The installer asks for runtime, PostgreSQL data, object, and backup directories, Owner details, and the local port. Press Enter to accept defaults.

### Docker Compose

After extracting the matching Release bundle, run `./install.sh` for first-time setup. Once initialized, the same configuration can be managed explicitly with Compose:

```bash
runtime_directory=/absolute/path/to/sbm-runtime
docker compose --project-name smart-bill-manager \
  --env-file "$runtime_directory/deployment.env" \
  --env-file infra/compose/release.env \
  -f infra/compose/compose.yaml \
  -f infra/compose/compose.release.yaml \
  up -d --no-build --pull never --wait app
```

The first provision, migration, and Owner bootstrap must still be run in order by `./install.sh`; do not skip them with a standalone `compose up`.

### Docker CLI (`docker run` style)

If PostgreSQL 17, least-privilege roles, the schema, and the Owner have already been prepared according to the deployment guide, the application container can be started with Docker CLI:

```bash
docker run -d \
  --name smart-bill-manager \
  --restart unless-stopped \
  --init \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=268435456 \
  --tmpfs /run/sbm-secrets:rw,noexec,nosuid,nodev,size=65536,mode=0700 \
  --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add SETGID --cap-add SETUID \
  --security-opt no-new-privileges:true \
  --pids-limit 256 --cpus 2 --memory 3584m --stop-timeout 20 \
  --network smart-bill-manager_database \
  -p 127.0.0.1:7476:8080 \
  -v /absolute/path/to/objects:/var/lib/sbm/objects \
  --mount type=bind,src=/absolute/path/to/master-key,dst=/run/secrets/sbm_master_key,readonly \
  --mount type=bind,src=/absolute/path/to/postgres-runtime-password,dst=/run/secrets/sbm_postgres_runtime_password,readonly \
  -e SBM_DEPLOYMENT_MODE=local \
  -e SBM_COOKIE_SECURE=false \
  -e SBM_SESSION_TTL=168h \
  -e SBM_AI_CONCURRENCY=2 \
  ghcr.io/tuoro/smart-bill-manager:v0.3.4
docker network connect bridge smart-bill-manager
```

Stop the Compose-managed app first to avoid a port conflict. This starts only the application container. It does not create PostgreSQL, roles, schema, or the Owner. It reuses the internal database network created by Compose, where PostgreSQL has the alias `database`, then joins the default bridge for outbound Provider access. Prefer the one-command installer or Compose for a complete installation. Open <http://127.0.0.1:7476> after successful startup. See the [deployment guide](docs/deployment.md) for complete boundaries; the detailed guide is maintained in Chinese.

## Database and persistence

The default Compose stack deploys PostgreSQL 17 as a separate container, provisions least-privilege roles, and initializes the database schema automatically. Regular users do not enter a database address or run SQL manually. The default layout keeps persistent material under the deployment directory; the installer can instead map the three data directories to separate new absolute paths:

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
