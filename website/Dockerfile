# syntax=docker/dockerfile:1
# The product website (ADR-0054). Build from the repository root:
#   docker build -f website/Dockerfile .
FROM oven/bun:1.4 AS web
WORKDIR /src/website
COPY website/package.json website/bun.lock* ./
RUN bun install --frozen-lockfile
COPY website/ ./
RUN bun run build

FROM golang:1.27 AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/website/dist ./website/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/phoedox-website ./cmd/phoedox-website

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/phoedox-website /usr/local/bin/phoedox-website
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/phoedox-website"]
