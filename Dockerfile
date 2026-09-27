# syntax=docker/dockerfile:1

# Multi-stage build producing two final images:
#
#   sqlagent          (default) — full server with the embedded DuckDB
#                     verification target (cgo), on distroless/cc. DuckDB is
#                     statically linked into the binary, so the cc variant of
#                     distroless (glibc + libstdc++) is required.
#   sqlagent-minimal  — static, CGO-free server without DuckDB, on
#                     distroless/static, under 30MB. Differential verification
#                     then needs the Snowflake target (build this variant with
#                     --build-arg TAGS="noduckdb snowflake").
#
# Embedded DuckDB is what keeps the default image above 30MB: the engine
# alone adds ~25MB. The minimal target is the size-compliant production path.

FROM golang:1.22-bookworm AS builder

ARG TAGS=""

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# UPX compresses the minimal static binary so the final image stays under
# 30MB (the pure-Go binary alone is ~25MB and distroless/static carries a
# ~13MB CA bundle for TLS to the LLM endpoint). Download the release binary
# so no distro package is needed.
RUN apt-get update && apt-get install -y --no-install-recommends xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && ARCH=$(dpkg --print-architecture) \
    && curl -fsSL "https://github.com/upx/upx/releases/download/v4.2.4/upx-4.2.4-${ARCH}_linux.tar.xz" \
       | tar -xJ --strip-components=1 -C /usr/local/bin "upx-4.2.4-${ARCH}_linux/upx" \
    && upx --version | head -1

# Full build: cgo DuckDB.
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o /out/sqlagent ./cmd/sqlagent

# Minimal build: pure Go, no DuckDB.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -tags "${TAGS} noduckdb" -o /out/sqlagent-minimal ./cmd/sqlagent \
    && upx -9 --lzma /out/sqlagent-minimal

# --- default target: full server -------------------------------------------
FROM gcr.io/distroless/cc-debian12:nonroot AS sqlagent

WORKDIR /app
COPY --from=builder /out/sqlagent /app/sqlagent
COPY configs /app/configs
COPY db /app/db

ENV SQLAGENT_PG_DSN=postgres://sqlagent:sqlagent@postgres:5432/tpch?sslmode=disable
ENV LLM_BASE_URL=http://host.docker.internal:11434/v1
ENV LLM_MODEL=llama3.2:3b

EXPOSE 8080
ENTRYPOINT ["/app/sqlagent", "serve"]

# --- minimal target: static, no DuckDB --------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS sqlagent-minimal

WORKDIR /app
COPY --from=builder /out/sqlagent-minimal /app/sqlagent
COPY configs /app/configs
COPY db /app/db

ENV SQLAGENT_PG_DSN=postgres://sqlagent:sqlagent@postgres:5432/tpch?sslmode=disable

EXPOSE 8080
ENTRYPOINT ["/app/sqlagent", "serve"]
