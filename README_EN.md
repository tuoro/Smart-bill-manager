# Smart Bill Manager

> **中文文档：** [README.md](README.md)

Smart Bill Manager is a self-hosted bill management system for individuals and small teams. It brings payment records, electronic invoices, business trips, and receipts from mailboxes into one place. The system supports automatic OCR extraction, per-user ledger isolation, administrator actions on behalf of users, and asynchronous task processing.

Current stable version: `v0.2.4`. This release tightens startup migrations and service boundaries, preserves email-log data during deduplication, enforces mailbox-key consistency, makes OCR worker shutdown interruptible, and adds production Compose delivery gates. See [CHANGELOG.md](CHANGELOG.md) for upgrade details and the [architecture document](docs/architecture.md) for design boundaries.

## Features

- Upload payment screenshots; recognize, categorize, filter, and summarize them with OCR
- Batch-upload PDF/image invoices; extract fields, deduplicate records, and match payments
- Monitor IMAP mailboxes and parse receipts from attachments and links in message bodies
- Assign business trips, process unassigned items, and track reimbursement and bad-debt status
- Register with invitation codes, isolate multi-user data, and require secondary confirmation for administrator actions on behalf of users
- Run asynchronous OCR jobs and cancel tasks
- Preview and download files through authenticated endpoints, with uploads isolated by user directory

## Technology Stack

- Backend: Go 1.24, Gin, GORM, SQLite, JWT, and emersion/go-imap
- Frontend: Vue 3, TypeScript, Vite, Pinia, PrimeVue, Axios, and ECharts
- OCR/PDF: RapidOCR v3, ONNX Runtime, PyMuPDF, and Poppler
- Deployment: Nginx, Supervisor, Docker Compose, GitHub Actions, and GHCR

## Quick Start

### Docker Compose

Production deployments use the `docker-compose.production.yml` override. It fixes `NODE_ENV=production` and refuses to create the container when `JWT_SECRET` is empty; the backend also rejects secrets shorter than 32 characters.

For a first deployment, create the environment file, generate a secret with `openssl rand -hex 32` (or a password manager), write the result to `JWT_SECRET` in `.env`, and retain that file or corresponding secret-manager entry:

```bash
cp .env.example .env
openssl rand -hex 32
# Write the previous command's output to JWT_SECRET in .env, then run:
docker compose -f docker-compose.yml -f docker-compose.production.yml config --quiet
docker compose -f docker-compose.yml -f docker-compose.production.yml up -d --build
```

On Windows PowerShell, use `Copy-Item .env.example .env`. `config --quiet` validates the configuration without printing the secret. After startup, open <http://localhost>; the first visit to `/setup` creates the administrator account. For local Compose development, you can still run `docker compose up --build` directly. The base file defaults to `development` when `NODE_ENV` is unset; do not use that entry point in production.

Default persistent volumes:

- `app-data`: SQLite database, mailbox-password encryption key, and OCR model cache
- `app-uploads`: payment screenshots, invoices, and email attachments

Check runtime status:

```bash
docker compose -f docker-compose.yml -f docker-compose.production.yml ps
docker compose -f docker-compose.yml -f docker-compose.production.yml logs -f smart-bill-manager
```

### Prebuilt Image

```bash
docker pull ghcr.io/tuoro/smart-bill-manager:0.2.4
docker run -d --name smart-bill-manager -p 80:80 \
  -e NODE_ENV=production \
  -e JWT_SECRET="replace-with-a-persistent-32-char-secret" \
  -e SBM_OCR_DATA_DIR=/app/backend/data \
  -e SBM_OCR_WORKER=1 \
  -v smart-bill-data:/app/backend/data \
  -v smart-bill-uploads:/app/backend/uploads \
  ghcr.io/tuoro/smart-bill-manager:0.2.4
```

Persist `JWT_SECRET` in production. Do not regenerate it at each startup, or every existing login session will become invalid.

## Upgrading from v0.1.0

Stop the service and create a backup while the checkout is still on v0.1.0, before pulling new code. v0.1.0 does not have the production override, so at this stage you must use its existing base `docker-compose.yml`. Preserve at least `bills.db`, `email_password.key`, and the entire uploads directory. Compose prefixes volume names with the project name, so the safest approach is to stop the service and copy the data from the existing container:

```bash
docker compose -f docker-compose.yml stop
mkdir -p backup-v0.1.0
docker cp smart-bill-manager:/app/backend/data ./backup-v0.1.0/data
docker cp smart-bill-manager:/app/backend/uploads ./backup-v0.1.0/uploads
```

After confirming that the backup is readable, pull the code and create or update `.env` from the new `.env.example`. If the existing deployment already has a persistent `JWT_SECRET`, keep the same value. Otherwise, generate a new secret once and retain it:

```bash
git pull --ff-only
[ -f .env ] || cp .env.example .env
# Reconcile .env with .env.example; run the next line only if no persistent secret exists.
openssl rand -hex 32
# Write the existing secret or the previous command's output to JWT_SECRET in .env, then run:
docker compose -f docker-compose.yml -f docker-compose.production.yml config --quiet
docker compose -f docker-compose.yml -f docker-compose.production.yml up -d --build
```

The service runs versioned migrations automatically at startup:

1. It checks the existing `schema_migrations` first, rejects future versions unsupported by the running program, and then synchronizes the schema.
2. It backfills owners, time-index fields, and split OCR data for legacy records.
3. It converts and validates monetary amounts as integer-cent fields.
4. It losslessly merges invoice associations and non-empty metadata from duplicate email logs before creating and validating the identity unique index.
5. Each versioned migration runs in its own transaction. Mailbox-password repair also uses a single transaction to validate existing ciphertext before encrypting plaintext; any failure prevents startup.

Migrations do not create external backups. v0.2.4 does not drop business tables or columns and does not discard email-log invoice associations; redundant email-log rows are deleted only after a successful merge in the same transaction. New and legacy amount fields continue to be written together throughout v0.2.x for a compatibility window, but a formal rollback still requires restoring a verified backup.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `3001` | Backend listen port |
| `NODE_ENV` | `development` in base Compose | The production override fixes this to `production` |
| `JWT_SECRET` | None | Required by the production override; the backend requires at least 32 characters, and the value must persist across restarts |
| `JWT_EXPIRES_IN` | `168h` | JWT lifetime |
| `CORS_ALLOWED_ORIGINS` | Development origins | Cross-origin allowlist; `*` is forbidden in production |
| `DATA_DIR` | `./data` | Directory for SQLite and local keys |
| `UPLOADS_DIR` | `./uploads` | Root directory for uploaded files |
| `SBM_OCR_WORKER` | `0` | Set to `1` to enable the resident OCR worker |
| `SBM_OCR_DATA_DIR` | None | OCR model-cache directory |
| `SBM_PDF_TEXT_EXTRACTOR` | `pymupdf` | PDF text extractor; may be set to `off` |
| `SBM_DRAFT_TTL_HOURS` | `6` | Draft retention period; `0` disables cleanup |
| `SBM_DRAFT_CLEANUP_INTERVAL_MINUTES` | `15` | Draft-cleanup interval |
| `SBM_TASK_PROCESSING_TTL_SECONDS` | `3600` | Timeout for tasks in the processing state |

Mailbox passwords are encrypted by default with `DATA_DIR/email_password.key`. You can instead provide a stable key through `SBM_EMAIL_PASSWORD_KEY` or `SBM_EMAIL_PASSWORD_KEY_FILE`. Replacing or losing the key makes stored mailbox passwords impossible to decrypt.

## Local Development

Requirements: Go 1.24, Node.js 24, npm, and Python 3. The full SQLite test suite also requires a C compiler because the driver depends on CGO.

Backend:

```bash
cd backend-go
go mod download
go run ./cmd/server
```

The frontend proxies `/api` to `http://localhost:3001`:

```bash
cd frontend
npm ci
npm run dev
```

Open <http://localhost:5173>.

## Quality Checks

```bash
cd backend-go
CGO_ENABLED=1 go test -count=1 -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
CGO_ENABLED=1 go vet ./...

cd ../frontend
npm ci
npm audit --audit-level=high
npm run lint:ci
npm run test:run
npm run build
```

CI requires zero frontend ESLint warnings, at least 35% overall backend statement coverage, and a complete unified Docker image build. Critical authentication, administrator-on-behalf-of, file-access, and payment/invoice association paths have real HTTP contract coverage, but the overall coverage percentage should not be interpreted as comprehensive coverage of every endpoint.

> Delivery follow-up: `Dockerfile` currently constrains only RapidOCR to `3.x`; pip still resolves the latest compatible releases of `onnxruntime`, Pillow, and PyMuPDF. Before pinning these Python/OCR dependencies precisely, build the image for the target architecture with an available Docker daemon, check `/api/health`, and run one real OCR smoke test. Do not perform speculative bulk upgrades before those checks pass.

## Project Structure

```text
Smart-bill-manager/
|- backend-go/
|  |- cmd/                 # Service and utility command entry points
|  |- internal/app/        # Application assembly and lifecycle
|  |- internal/handlers/   # HTTP adapter layer
|  |- internal/services/   # Business logic and transaction orchestration
|  |- internal/repository/ # Data access
|  |- internal/migrations/ # Versioned migrations
|  `- pkg/database/        # SQLite connection
|- frontend/src/
|  |- api/                 # Request client, storage, and domain APIs
|  |- stores/              # Cross-page session state
|  |- composables/         # Reusable asynchronous flows
|  |- components/          # Domain components
|  `- views/               # Routed pages
|- docs/architecture.md
|- Dockerfile
|- docker-compose.yml
`- docker-compose.production.yml
```

## Security Boundaries

Report security vulnerabilities privately according to [SECURITY.md](SECURITY.md). Do not disclose them in a public Issue.

- All business data is queried by `owner_user_id`; write operations performed by an administrator on behalf of a user require secondary confirmation.
- The uploads directory is not exposed as a public static directory; previews and downloads must pass through authenticated endpoints.
- API 5xx responses do not return internal error details; full causes are written only to server logs.
- Production rejects empty or too-short JWT secrets and wildcard CORS origins.

## License

MIT License. See [LICENSE](LICENSE).
