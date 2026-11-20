FROM node:22-bookworm-slim AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
COPY prototype/src/style.css /src/prototype/src/style.css
RUN npm run build

FROM golang:1.25-bookworm AS backend
WORKDIR /src
ENV CGO_ENABLED=1
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/clash-studio ./cmd/clash-studio

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && install -d -m 0700 -o 10001 -g 10001 /data
WORKDIR /app
COPY --from=backend /out/clash-studio /app/clash-studio
COPY --from=frontend /src/frontend/dist/ /app/frontend/
ENV ADDR=:8080 \
    FRONTEND_DIR=/app/frontend \
    DATA_DIR=/data
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["curl", "-fsS", "--max-time", "3", "-o", "/dev/null", "http://127.0.0.1:8080/healthz"]
ENTRYPOINT ["/app/clash-studio"]
