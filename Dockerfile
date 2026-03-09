FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web-builder

WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . ./
COPY --from=web-builder /src/web/dist ./web/dist

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/nekoclaw ./cmd/nekoclaw

FROM node:22-bookworm-slim AS runtime

ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates; \
    mkdir -p /app /data/.nekoclaw /ms-playwright; \
    PW_VERSION="$(npm view @playwright/mcp@latest dependencies.playwright)"; \
    PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 npm install -g "@google/gemini-cli@latest" "@playwright/mcp@latest" "playwright@${PW_VERSION}"; \
    playwright install --with-deps chromium; \
    addgroup --system nekoclaw; \
    adduser --system --home /data --ingroup nekoclaw nekoclaw; \
    chown -R nekoclaw:nekoclaw /app /data /ms-playwright; \
    npm cache clean --force; \
    rm -rf /var/lib/apt/lists/*

ENV HOME=/data \
    NEKOCLAW_AUTH_DIR=/data/.nekoclaw/auth \
    NEKOCLAW_SESSIONS_DIR=/data/.nekoclaw/sessions \
    NEKOCLAW_MEMORY_DIR=/data/.nekoclaw/memory \
    NEKOCLAW_MCP_DIR=/data/.nekoclaw/mcp \
    NEKOCLAW_PERSONAS_DIR=/data/.nekoclaw/personas

WORKDIR /app

COPY --from=builder /out/nekoclaw /usr/local/bin/nekoclaw

USER nekoclaw

EXPOSE 8085
VOLUME ["/data/.nekoclaw"]

ENTRYPOINT ["/usr/local/bin/nekoclaw"]
CMD ["-mode", "web", "-addr", "0.0.0.0:8085"]
