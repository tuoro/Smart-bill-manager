ARG ALPINE_IMAGE=alpine:3.23
ARG GLIBC_SOURCE_IMAGE=smart-bill-manager:go-glibc-source-local
ARG POSTGRES_IMAGE=postgres:17-alpine

FROM ${GLIBC_SOURCE_IMAGE} AS glibc-source
FROM ${POSTGRES_IMAGE} AS postgres-tools

FROM ${ALPINE_IMAGE} AS artifact-validator
ARG VCS_REF
ARG RELEASE_INPUT_SHA256
WORKDIR /release
COPY --from=release_artifacts / ./
RUN set -eu; \
    grep -Fx "baseline_head=${VCS_REF}" identity.env >/dev/null; \
    grep -Fx "release_input_sha256=${RELEASE_INPUT_SHA256}" identity.env >/dev/null; \
    grep -Fx 'node_version=v24.19.0' identity.env >/dev/null; \
    grep -Fx 'go_version=go1.26.7' identity.env >/dev/null; \
    grep -Fx 'go_image_id=sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57' identity.env >/dev/null; \
    grep -Fx 'glibc_source_image_id=sha256:dc2521c2a906db43073b8b4d99f491b6341cf15610b6ebbab187c45153f9959e' identity.env >/dev/null; \
    grep -Fx 'poppler_version=26.05.0' identity.env >/dev/null; \
    grep -Fx 'poppler_source_sha256=6fef27ff04f37db43054c86bcdff6128c9fb1f6af4ef3c8b369a7e9abd68d0bb' identity.env >/dev/null; \
    test -z "$(find . ! -type d ! -type f -print -quit)"; \
    sha256sum -c SHA256SUMS >/dev/null; \
    find . -type f ! -name SHA256SUMS -print | sed 's#^\./##' | LC_ALL=C sort >/tmp/actual-files; \
    awk '{print $2}' SHA256SUMS | LC_ALL=C sort >/tmp/expected-files; \
    cmp /tmp/actual-files /tmp/expected-files

FROM ${ALPINE_IMAGE} AS production
ARG VCS_REF
ARG RELEASE_INPUT_SHA256
RUN printf '%s' "${VCS_REF}" | grep -Eq '^[0-9a-f]{40}$' \
    && printf '%s' "${RELEASE_INPUT_SHA256}" | grep -Eq '^[0-9a-f]{64}$' \
    && addgroup -S -g 10001 sbm \
    && adduser -S -D -H -u 10001 -G sbm sbm \
    && mkdir -p /var/lib/sbm/objects /run/sbm-secrets \
    && chown -R sbm:sbm /var/lib/sbm \
    && chown root:sbm /run/sbm-secrets \
    && chmod 0700 /var/lib/sbm/objects \
    && chmod 0710 /run/sbm-secrets \
    && rm -f /sbin/apk
LABEL org.opencontainers.image.title="Smart Bill Manager" \
      org.opencontainers.image.revision="${VCS_REF}" \
      com.smart-bill-manager.release-input-sha256="${RELEASE_INPUT_SHA256}" \
      com.smart-bill-manager.node-build-version="24.19.0" \
      com.smart-bill-manager.go-build-version="go1.26.7" \
      com.smart-bill-manager.glibc-source-image-id="sha256:dc2521c2a906db43073b8b4d99f491b6341cf15610b6ebbab187c45153f9959e" \
      com.smart-bill-manager.runtime-contract="alpine-3.23-glibc-2.41-poppler-26.05-tzdata/1" \
      org.opencontainers.image.description="Clean Slate Source-Claim-Fact local release candidate"

WORKDIR /app
COPY --from=glibc-source /usr/lib/x86_64-linux-gnu/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2
COPY --from=glibc-source /usr/lib/x86_64-linux-gnu/libc.so.6 /usr/lib/x86_64-linux-gnu/libc.so.6
COPY --from=glibc-source /usr/lib/x86_64-linux-gnu/libm.so.6 /usr/lib/x86_64-linux-gnu/libm.so.6
COPY --from=glibc-source /usr/lib/x86_64-linux-gnu/libdl.so.2 /usr/lib/x86_64-linux-gnu/libdl.so.2
COPY --from=glibc-source /usr/lib/x86_64-linux-gnu/libpthread.so.0 /usr/lib/x86_64-linux-gnu/libpthread.so.0
COPY --from=glibc-source /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=artifact-validator --chmod=0755 /release/server /app/server
COPY --from=artifact-validator --chmod=0755 /release/bootstrap-owner /app/bootstrap-owner
COPY --from=artifact-validator --chmod=0755 /release/backup /app/backup
COPY --from=artifact-validator --chmod=0755 /release/migrate /app/migrate
COPY --from=artifact-validator --chmod=0755 /release/provision-postgresql /app/provision-postgresql
COPY --from=artifact-validator --chmod=0755 /release/run-as-sbm /app/run-as-sbm
COPY --from=artifact-validator /release/web /app/web
COPY --from=artifact-validator /release/poppler /opt/sbm-poppler
COPY infra/migrations /app/migrations
COPY contracts/schemas/bill-visible-text.schema.json /app/contracts/bill-visible-text.schema.json
COPY --chmod=0755 infra/docker/entrypoint.sh /usr/local/bin/sbm-entrypoint
COPY --from=postgres-tools /usr/local/bin/pg_dump /usr/local/bin/pg_dump
COPY --from=postgres-tools /usr/local/bin/pg_restore /usr/local/bin/pg_restore
COPY --from=postgres-tools /usr/local/lib/libpq.so.5 /usr/local/lib/libpq.so.5
COPY --from=postgres-tools /usr/lib/libzstd.so.1 /usr/lib/libzstd.so.1
COPY --from=postgres-tools /usr/lib/liblz4.so.1 /usr/lib/liblz4.so.1
COPY --from=postgres-tools /usr/lib/libcrypto.so.3 /usr/lib/libcrypto.so.3
COPY --from=postgres-tools /usr/lib/libz.so.1 /usr/lib/libz.so.1
COPY --from=postgres-tools /usr/lib/libssl.so.3 /usr/lib/libssl.so.3
COPY --from=postgres-tools /usr/lib/libgssapi_krb5.so.2 /usr/lib/libgssapi_krb5.so.2
COPY --from=postgres-tools /usr/lib/libldap.so.2 /usr/lib/libldap.so.2
COPY --from=postgres-tools /usr/lib/libkrb5.so.3 /usr/lib/libkrb5.so.3
COPY --from=postgres-tools /usr/lib/libk5crypto.so.3 /usr/lib/libk5crypto.so.3
COPY --from=postgres-tools /usr/lib/libcom_err.so.2 /usr/lib/libcom_err.so.2
COPY --from=postgres-tools /usr/lib/libkrb5support.so.0 /usr/lib/libkrb5support.so.0
COPY --from=postgres-tools /usr/lib/liblber.so.2 /usr/lib/liblber.so.2
COPY --from=postgres-tools /usr/lib/libsasl2.so.3 /usr/lib/libsasl2.so.3
COPY --from=postgres-tools /usr/lib/libkeyutils.so.1 /usr/lib/libkeyutils.so.1

ENV SBM_POSTGRES_HOST=database \
    SBM_POSTGRES_PORT=5432 \
    SBM_POSTGRES_DATABASE=smart_bill_manager \
    SBM_POSTGRES_USER=sbm_runtime \
    SBM_POSTGRES_PASSWORD_FILE=/run/sbm-secrets/postgres-runtime-password \
    SBM_POSTGRES_SSL_MODE=disable \
    SBM_POSTGRES_MAX_OPEN_CONNECTIONS=32 \
    SBM_MIGRATIONS_DIR=/app/migrations \
    SBM_HTTP_ADDRESS=0.0.0.0:8080 \
    SBM_OBJECTS_PATH=/var/lib/sbm/objects \
    SBM_PDFINFO_PATH=/opt/sbm-poppler/bin/pdfinfo \
    SBM_PDFTOPPM_PATH=/opt/sbm-poppler/bin/pdftoppm \
    SBM_MASTER_KEY_FILE=/run/sbm-secrets/master-key \
    SBM_EXTRACTION_SCHEMA_PATH=/app/contracts/bill-visible-text.schema.json \
    SBM_WEB_DIST_PATH=/app/web \
    FONTCONFIG_FILE=/opt/sbm-poppler/etc/fonts/fonts.conf \
    POPPLER_DATADIR=/opt/sbm-poppler/share/poppler

USER root
VOLUME ["/var/lib/sbm/objects"]
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=6 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/api/v1/ready || exit 1
ENTRYPOINT ["/usr/local/bin/sbm-entrypoint"]
CMD ["/app/server"]
