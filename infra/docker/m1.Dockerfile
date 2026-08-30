# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=node:24.19.0-alpine3.23
ARG GO_IMAGE=golang:1.26.7-alpine3.23
ARG RUNTIME_IMAGE=alpine:3.23

FROM ${NODE_IMAGE} AS web-builder
WORKDIR /workspace/apps/web
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY contracts /workspace/contracts
COPY apps/web ./
RUN npm run build

FROM ${GO_IMAGE} AS api-builder
WORKDIR /workspace/apps/api
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bootstrap-owner ./cmd/bootstrap-owner

FROM ${RUNTIME_IMAGE} AS production
ARG VCS_REF=unknown
LABEL org.opencontainers.image.title="Smart Bill Manager M1" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.description="Clean Slate Source-Claim-Fact runtime"

RUN apk add --no-cache ca-certificates poppler-utils su-exec tzdata \
    && addgroup -S -g 10001 sbm \
    && adduser -S -D -H -u 10001 -G sbm sbm \
    && mkdir -p /var/lib/sbm/database /var/lib/sbm/objects /run/sbm-secrets \
    && chown -R sbm:sbm /var/lib/sbm /run/sbm-secrets \
    && chmod 0700 /var/lib/sbm/database /var/lib/sbm/objects /run/sbm-secrets

WORKDIR /app
COPY --from=api-builder /out/server /app/server
COPY --from=api-builder /out/bootstrap-owner /app/bootstrap-owner
COPY --from=web-builder /workspace/apps/web/dist /app/web
COPY infra/migrations /app/migrations
COPY contracts/schemas/bill-visible-text.schema.json /app/contracts/bill-visible-text.schema.json
COPY --chmod=0755 infra/docker/m1-entrypoint.sh /usr/local/bin/m1-entrypoint

ENV SBM_DATABASE_PATH=/var/lib/sbm/database/sbm.sqlite \
    SBM_MIGRATIONS_DIR=/app/migrations \
    SBM_HTTP_ADDRESS=0.0.0.0:8080 \
    SBM_OBJECTS_PATH=/var/lib/sbm/objects \
    SBM_PDFINFO_PATH=/usr/bin/pdfinfo \
    SBM_PDFTOPPM_PATH=/usr/bin/pdftoppm \
    SBM_MASTER_KEY_FILE=/run/sbm-secrets/master-key \
    SBM_MASTER_KEY_SOURCE_FILE=/run/secrets/sbm_master_key \
    SBM_EXTRACTION_SCHEMA_PATH=/app/contracts/bill-visible-text.schema.json \
    SBM_WEB_DIST_PATH=/app/web

VOLUME ["/var/lib/sbm/database", "/var/lib/sbm/objects"]
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=6 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/api/v1/ready || exit 1
ENTRYPOINT ["/usr/local/bin/m1-entrypoint"]
CMD ["/app/server"]
