FROM golang:1.25-alpine AS builder
WORKDIR /src

ARG TARGETOS=linux
ARG TARGETARCH=amd64

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/proxer-gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /out/proxer-agent ./cmd/agent

FROM alpine:3.20 AS runtime-base
RUN apk add --no-cache ca-certificates sqlite && adduser -D -H proxer && mkdir -p /data && chown -R proxer:proxer /data

USER proxer
EXPOSE 8080

FROM runtime-base AS gateway-runtime
COPY --from=builder /out/proxer-gateway /usr/local/bin/proxer-gateway
ENTRYPOINT ["proxer-gateway"]

FROM runtime-base AS agent-runtime
COPY --from=builder /out/proxer-agent /usr/local/bin/proxer-agent
ENTRYPOINT ["proxer-agent"]

FROM runtime-base AS dev-runtime
COPY --from=builder /out/proxer-gateway /usr/local/bin/proxer-gateway
COPY --from=builder /out/proxer-agent /usr/local/bin/proxer-agent

FROM dev-runtime
