FROM golang:1.27-bookworm AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY internal/ ./internal/
COPY cmd/presence-processor ./cmd/presence-processor

RUN CGO_ENABLED=0 GOOS=linux go build -o /presence-processor ./cmd/presence-processor

FROM scratch

COPY --from=build /presence-processor /presence-processor

ENTRYPOINT ["/presence-processor"]