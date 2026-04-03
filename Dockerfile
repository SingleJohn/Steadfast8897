# ====== Stage 1: Build Frontend ======
FROM node:22-slim AS frontend-builder

WORKDIR /build/web
COPY web/ ./
RUN if [ -f dist/index.html ]; then \
      echo "Using prebuilt frontend assets from web/dist"; \
    else \
      npm ci && npm run build; \
    fi

# ====== Stage 2: Build Backend (Go) ======
FROM golang:1.23-alpine AS backend-builder

RUN apk add --no-cache git

ARG BUILD_VERSION=""
ARG BUILD_COMMIT=""
ARG BUILD_TIME=""
ARG BUILD_REPO=""

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY internal/ ./internal/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w \
      -X fyms/internal/config.BuildVersion=${BUILD_VERSION} \
      -X fyms/internal/config.BuildCommit=${BUILD_COMMIT} \
      -X fyms/internal/config.BuildTime=${BUILD_TIME} \
      -X fyms/internal/config.BuildRepo=${BUILD_REPO}" \
    -o fyms .

# ====== Stage 3: Runtime ======
FROM debian:12-slim

RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null; \
    sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list 2>/dev/null; \
    apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates ffmpeg \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -u 1000 fyms

WORKDIR /app

COPY --from=backend-builder /build/fyms /app/fyms
COPY --from=frontend-builder /build/web/dist /app/web/dist
COPY migrations /app/migrations
RUN mkdir -p /app/data/logs /app/data/cache/images && chown -R fyms:fyms /app

RUN printf '#!/bin/sh\nmkdir -p /app/data/logs /app/data/cache/images 2>/dev/null\nexec /app/fyms "$@"\n' > /app/entrypoint.sh \
    && chmod +x /app/entrypoint.sh

USER fyms

VOLUME ["/app/data"]

EXPOSE 8961

ENTRYPOINT ["/app/entrypoint.sh"]
