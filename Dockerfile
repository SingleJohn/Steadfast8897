# ====== Stage 1: Build ======
FROM rust:1.88-slim AS builder

RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null; \
    sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list 2>/dev/null; \
    apt-get update && apt-get install -y --no-install-recommends \
    pkg-config libssl-dev build-essential \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build
COPY Cargo.toml Cargo.lock ./
COPY src ./src

# Build release binary
RUN cargo build --release && strip target/release/fyms-rs

# ====== Stage 2: Runtime ======
FROM debian:12-slim

RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null; \
    sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list 2>/dev/null; \
    apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates ffmpeg \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -u 1000 fyms

WORKDIR /app

COPY --from=builder /build/target/release/fyms-rs /app/fyms-rs
COPY web/dist /app/web/dist
COPY migrations /app/migrations
RUN mkdir -p /app/data/logs /app/data/cache/images && chown -R fyms:fyms /app

# Entrypoint script: ensure data dirs are writable, then exec app
RUN printf '#!/bin/sh\nmkdir -p /app/data/logs /app/data/cache/images 2>/dev/null\nexec /app/fyms-rs "$@"\n' > /app/entrypoint.sh \
    && chmod +x /app/entrypoint.sh

USER fyms

VOLUME ["/app/data"]

EXPOSE 8961

ENTRYPOINT ["/app/entrypoint.sh"]
