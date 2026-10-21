FROM golang:1.27.0-alpine3.24 AS builder

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/nexus-api \
    ./cmd/api


RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/nexus-outbox-publisher \
    ./cmd/outbox-publisher

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/nexus-delivery-dispatcher \
    ./cmd/delivery-dispatcher

FROM alpine:3.24.1 AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 nexus \
    && adduser -S -D -H -u 10001 -G nexus nexus

WORKDIR /app

COPY --from=builder --chown=nexus:nexus \
    /out/nexus-api \
    /app/nexus-api


COPY --from=builder --chown=nexus:nexus \
    /out/nexus-outbox-publisher \
    /app/nexus-outbox-publisher

COPY --from=builder --chown=nexus:nexus \
    /out/nexus-delivery-dispatcher \
    /app/nexus-delivery-dispatcher

USER nexus:nexus

EXPOSE 8080

ENTRYPOINT ["/app/nexus-api"]
