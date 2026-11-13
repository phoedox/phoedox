# syntax=docker/dockerfile:1
FROM oven/bun:1.1 AS web
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
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/phx ./cmd/phx

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/ /usr/local/bin/
ENV PHOEDOX_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/phoedox"]
