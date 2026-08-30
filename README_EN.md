# Smart Bill Manager

> **中文：** [README.md](README.md)

> [!IMPORTANT]
> M0, M1, and M2 are complete. All five M2 slices—amount allocation, deterministic duplicate detection, cross-page review, batch upload, and confirmed-Fact allocation adjustment—have passed acceptance. The new system does not preserve v0.x compatibility or read or migrate legacy data.

Smart Bill Manager is a self-hosted AI financial-document workspace for individuals and small teams. After a user uploads a payment screenshot or invoice, the system produces verifiable candidate fields and evidence. A candidate becomes a formal financial fact only after explicit user confirmation.

## Non-negotiable invariants

```text
Source -> Claim -> Fact
original evidence -> AI candidate -> user-confirmed data
```

- A new upload never overwrites the original Source.
- AI may produce Claims but may not create or mutate Facts directly.
- JSON Schema, deterministic business validation, tenant authorization, and human review are all mandatory.
- The first stage has one OpenAI-compatible Chat Completions transport implementation, with no vendor branching, multi-model routing, or automatic fallback.
- The new system does not depend on `backend-go/`, `frontend/`, the legacy OCR pipeline, legacy databases, or legacy Compose files.
- Software tests use versioned synthetic data; model-quality evaluation uses protected Chinese real-world assets. There is no production runtime regression-sample module.

## Current stage

M0 was completed on 2026-08-27 and froze this executable design baseline:

- product, scope, acceptance, architecture, AI, data-model, and UI/UX specifications;
- approved quantitative thresholds and their measurement protocols;
- visual direction 02, “Chinese enterprise workbench,” and four representative pages;
- independent workspace, baseline SHA, scoped diff, responsive, and accessibility evidence;
- an independent read-only review.

The independent read-only review found no M0 blocker, major, or minor issue; see the [M0 evidence](docs/m0-evidence.md). The [M1 AI inbox vertical slice](docs/scope.md) was completed on 2026-08-30; see the [M1 evidence](docs/m1-evidence.md). M2 completed five slices on 2026-08-31, bringing amount allocation, suspected duplicates, cross-page review, batch upload, and atomic confirmed-Fact allocation adjustment into the same Clean Slate flow; see the [M2 evidence](docs/m2-evidence.md). M3's first email Source, message, and attachment archive slice is complete; see the [M3 evidence](docs/m3-evidence.md). The next slice starts by freezing trip-attribution scope, while real mailbox connections, credentials, and integration remain outside the authorization gate. Clear, complete, unobstructed, and directly human-readable uploads remain the input commitment, while model-accuracy hardening is deferred until all local functional development is complete.

## Authoritative documents

| Document                                                | Purpose                                                                |
| ------------------------------------------------------- | ---------------------------------------------------------------------- |
| [Product baseline](docs/product.md)                     | Positioning, users, and the first journey                              |
| [Scope and non-goals](docs/scope.md)                    | Clean Slate and milestone boundaries                                   |
| [Acceptance criteria](docs/acceptance.md)               | Quantitative gates, protocols, and failure rules                       |
| [Architecture baseline](docs/architecture.md)           | Module boundaries, dependency direction, and state machines            |
| [AI pipeline](docs/ai-pipeline.md)                      | Provider, Schema, validation, review, and failure rules                |
| [Data model](docs/data-model.md)                        | Source, Claim, Fact, provenance, and deletion rules                    |
| [UI/UX baseline](docs/ui-ux.md)                         | The sole visual direction, four-page structure, and accessibility      |
| [M0 evidence](docs/m0-evidence.md)                      | Workspace, responsive, WCAG, link, and review evidence                 |
| [M1 evidence](docs/m1-evidence.md)                      | Executed gates, real-model diagnostics, and the current stage decision |
| [M2 evidence](docs/m2-evidence.md)                      | Five slice implementations, invariants, and executed gates             |
| [M3 evidence](docs/m3-evidence.md)                      | Email archive slice implementation, invariants, and executed gates     |
| [M1 backup and restore runbook](docs/backup-restore.md) | Offline consistent snapshots, manifests, restore, and retention policy |
| [Roadmap](docs/roadmap.md)                              | Milestones and entry gates                                             |

Key decisions include:

- [ADR-0001: Clean Slate rebuild](docs/decisions/0001-clean-slate.md)
- [ADR-0002: Source, Claim, and Fact boundary](docs/decisions/0002-source-claim-fact.md)
- [ADR-0003: one OpenAI-compatible adapter in the first stage](docs/decisions/0003-openai-compatible.md)
- [ADR-0004: separate the Provider generation schema from the authoritative local schema](docs/decisions/0004-provider-schema-projection.md)
- [ADR-0009: payment-to-invoice amount allocation](docs/decisions/0009-payment-invoice-allocation.md)
- [ADR-0010: deterministic duplicate detection](docs/decisions/0010-deterministic-duplicate-detection.md)
- [ADR-0011: cross-page invoice review](docs/decisions/0011-cross-page-invoice-review.md)
- [ADR-0012: client-orchestrated batch upload](docs/decisions/0012-client-orchestrated-batch-upload.md)
- [ADR-0013: confirmed-Fact allocation adjustment](docs/decisions/0013-confirmed-fact-allocation-adjustment.md)
- [ADR-0014: connector-neutral email Source and immutable archive](docs/decisions/0014-connector-neutral-email-archive.md)

## Target directories

The Clean Slate implementation lives in:

```text
apps/api/
apps/web/
contracts/
infra/
tests/
tools/
```

The repository's `backend-go/`, `frontend/`, and legacy deployment files are unchanged legacy references during M0. They are not the new system's runtime or acceptance entry point. Use a historical Release when the old product is needed; the new system offers no upgrade, import, or compatibility promise.

## Security

Report vulnerabilities privately according to [SECURITY.md](SECURITY.md), not in a public Issue.

## License

MIT License. See [LICENSE](LICENSE).
