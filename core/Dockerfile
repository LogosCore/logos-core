ARG GO_VERSION=1.25
ARG ALPINE_VERSION=3.20

FROM golang:${GO_VERSION}-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
        -trimpath \
        -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
        -o /out/logos-core \
        .


# logos-vpn's binaries, the release vpn-client.version names, checked
# against its SHA256SUMS: what the VPN tab offers for download.
FROM alpine:${ALPINE_VERSION} AS vpnclients

RUN apk add --no-cache ca-certificates wget
COPY vpn-client.version /src/vpn-client.version
COPY scripts/fetch-vpn-clients.sh /src/scripts/fetch-vpn-clients.sh
RUN /src/scripts/fetch-vpn-clients.sh /out/vpn-clients


FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S app \
    && adduser -S -G app -u 10001 app

WORKDIR /app

COPY --from=builder /out/logos-core /app/logos-core
COPY --from=vpnclients /out/vpn-clients /app/vpn-clients

USER app

EXPOSE 8002

ENTRYPOINT ["/app/logos-core"]
