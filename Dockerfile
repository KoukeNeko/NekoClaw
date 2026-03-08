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

FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S nekoclaw \
    && adduser -S -D -h /data -G nekoclaw nekoclaw \
    && mkdir -p /app /data/.nekoclaw \
    && chown -R nekoclaw:nekoclaw /app /data

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
