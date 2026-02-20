FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/proxer-gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/proxer-agent ./cmd/agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates sqlite && adduser -D -H proxer && mkdir -p /data && chown -R proxer:proxer /data

COPY --from=builder /out/proxer-gateway /usr/local/bin/proxer-gateway
COPY --from=builder /out/proxer-agent /usr/local/bin/proxer-agent

USER proxer
EXPOSE 8080
