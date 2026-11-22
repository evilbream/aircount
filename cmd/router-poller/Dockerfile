FROM golang:1.27-bookworm AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY internal/ ./internal/
COPY cmd/router-poller ./cmd/router-poller

RUN CGO_ENABLED=0 GOOS=linux go build -o /router-poller ./cmd/router-poller

FROM scratch

COPY --from=build /router-poller /router-poller
EXPOSE 8083

ENTRYPOINT ["/router-poller"]