ARG GO_VERSION=1.25
ARG ALPINE_VERSION=3.20
# Where the binary comes from: built here from source (builder, the
# default), or by CI before the image, with Go's build cache (prebuilt:
# publish-core.yml builds it into ci-bin/, a minute less than here).
ARG BINARY_FROM=builder

FROM golang:${GO_VERSION}-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

# Our own modules come straight from GitHub (git), not through the public Go
# proxy: a tag builds the moment it is pushed. go.sum still checks them.
ENV GOPRIVATE=github.com/logoscore/*

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

# The binary CI built, in ci-bin/. The directory is committed (.keep) so a
# builder that builds every stage, Docker's classic one, finds it; BuildKit
# builds only the stage BINARY_FROM names.
FROM scratch AS prebuilt
COPY ci-bin/ /out/

FROM ${BINARY_FROM} AS binary

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

COPY --from=binary /out/logos-core /app/logos-core
COPY --from=vpnclients /out/vpn-clients /app/vpn-clients

USER app

EXPOSE 8002

ENTRYPOINT ["/app/logos-core"]
