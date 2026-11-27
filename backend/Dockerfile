# parallel-api image. Build context is ./backend:
#   docker build -t parallel-api ./backend
# Server (default):  docker run -p 8080:8080 -e DATABASE_URL=... parallel-api
# Migrations:        docker run --rm -e MIGRATE_DATABASE_URL=... parallel-api migrate up
# No secrets or model identifiers are baked in; everything comes from the environment.

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Migrations (migrations/*.sql) and scenarios are embedded with go:embed.
RUN CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /out/parallel-api ./cmd/parallel-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/parallel-api /parallel-api
ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/parallel-api", "healthcheck"]
ENTRYPOINT ["/parallel-api"]
