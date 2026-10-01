# Imagem de backup/restore (task 0043, ADR 0013): pg_dump | age | rclone em
# stream. Versões fixas (reprodutível; ARM64 e AMD64). Contexto de build:
# ./scripts/backup (ver compose).
#
# rclone vem do release oficial com SHA256 fixado por arquitetura: o pacote do
# Alpine 3.22 (1.69.3) acumula advisories corrigidos só na 1.75.1 (OSV,
# 2026-10-01) — revisar junto com a tag do Alpine.
FROM alpine:3.22.6 AS rclone
ARG TARGETARCH
ARG RCLONE_VERSAO=1.75.1
ARG RCLONE_SHA256_AMD64=982b5aa772841168f8e380f139e9e787b2a105403e32b94da8676a0e1c0a13ab
ARG RCLONE_SHA256_ARM64=03f2504174034b6d004152ed7369251c9a9ec1f7e0836eda420f5c7a5ec0dff9
RUN set -eu; \
    arch="${TARGETARCH:-$(apk --print-arch | sed 's/x86_64/amd64/; s/aarch64/arm64/')}"; \
    case "$arch" in \
      amd64) sha="$RCLONE_SHA256_AMD64" ;; \
      arm64) sha="$RCLONE_SHA256_ARM64" ;; \
      *) echo "arquitetura sem checksum: $arch" >&2; exit 1 ;; \
    esac; \
    zip="rclone-v${RCLONE_VERSAO}-linux-${arch}.zip"; \
    wget -q -O "/tmp/$zip" "https://downloads.rclone.org/v${RCLONE_VERSAO}/$zip"; \
    echo "$sha  /tmp/$zip" | sha256sum -c -; \
    unzip -q -j "/tmp/$zip" "*/rclone" -d /usr/local/bin; \
    chmod 0555 /usr/local/bin/rclone

FROM alpine:3.22.6

RUN apk add --no-cache \
      bash \
      curl \
      age=1.2.1-r10 \
      postgresql16-client=16.15-r0 \
    && addgroup -g 10001 -S backup \
    && adduser -u 10001 -S -G backup -h /home/backup backup

COPY --from=rclone /usr/local/bin/rclone /usr/local/bin/rclone
COPY *.sh invariantes.sql /opt/backup/
RUN chmod 0555 /opt/backup/*.sh && chmod 0444 /opt/backup/invariantes.sql

USER backup
WORKDIR /home/backup
CMD ["bash", "/opt/backup/agendar.sh"]
