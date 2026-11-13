# syntax=docker/dockerfile:1
FROM oven/bun:1.4 AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock* ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

FROM golang:1.27 AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/phoedox/phoedox/internal/server.Version=${VERSION}" -o /out/phoedox ./cmd/phoedox \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/phx ./cmd/phx \
 && mkdir -p /data

# Variant with the optional file tools: PDF previews and text (poppler),
# OCR (tesseract, German + English). Build with --target full.
FROM debian:13-slim AS full
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates poppler-utils tesseract-ocr tesseract-ocr-deu tesseract-ocr-eng \
 && rm -rf /var/lib/apt/lists/* && useradd --uid 65532 --create-home nonroot
COPY --from=server /out/ /usr/local/bin/
COPY --from=server --chown=65532:65532 /data /data
USER 65532:65532
ENV PHOEDOX_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/phoedox"]

# Default: minimal image without external tools (last stage = default target).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/ /usr/local/bin/
# The volume must be writable by the nonroot user (SQLite, uploads).
COPY --from=server --chown=65532:65532 /data /data
ENV PHOEDOX_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/phoedox"]
