FROM golang:1.27-bookworm AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY internal/ ./internal/
COPY cmd/alert ./cmd/alert

RUN CGO_ENABLED=0 GOOS=linux go build -o /alert ./cmd/alert

FROM scratch

COPY --from=build /alert /alert

ENTRYPOINT ["/alert"]
