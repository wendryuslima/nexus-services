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

FROM alpine:3.24.1 AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 nexus \
    && adduser -S -D -H -u 10001 -G nexus nexus

WORKDIR /app

COPY --from=builder --chown=nexus:nexus \
    /out/nexus-api \
    /app/nexus-api

USER nexus:nexus

EXPOSE 8080

ENTRYPOINT ["/app/nexus-api"]