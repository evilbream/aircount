FROM golang:1.27-bookworm AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY internal/ ./internal/
COPY cmd/simple-ws ./cmd/simple-ws

RUN CGO_ENABLED=0 GOOS=linux go build -o /simple-ws ./cmd/simple-ws

FROM scratch

COPY --from=build /simple-ws /simple-ws
EXPOSE 8088

ENTRYPOINT ["/simple-ws"]