FROM golang:1.26-bookworm AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY internal/ ./internal/
COPY cmd/ingest-gateway ./cmd/ingest-gateway

RUN CGO_ENABLED=0 GOOS=linux go build -o /ingest-gateway ./cmd/ingest-gateway

FROM scratch

COPY --from=build /ingest-gateway /ingest-gateway
EXPOSE 8080

ENTRYPOINT ["/ingest-gateway"]