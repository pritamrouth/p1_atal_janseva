# ── Build stage ────────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /ataljanseva-bot ./cmd/server

# ── Runtime stage ──────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12
COPY --from=build /ataljanseva-bot /ataljanseva-bot
# Migrations are applied on startup (complaints / meetings tables).
COPY --from=build /src/migrations /migrations
ENV MIGRATIONS_DIR=/migrations

EXPOSE 8080
ENTRYPOINT ["/ataljanseva-bot"]
