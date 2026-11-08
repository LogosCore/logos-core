FROM golang:1.25

RUN go install github.com/air-verse/air@latest

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# logos-vpn's binaries (vpn-client.version), outside /app, which the
# source mount covers: compose points VPN_CLIENT_DIR here.
COPY vpn-client.version /opt/vpn/vpn-client.version
COPY scripts/fetch-vpn-clients.sh /opt/vpn/scripts/fetch-vpn-clients.sh
RUN /opt/vpn/scripts/fetch-vpn-clients.sh /opt/vpn-clients

CMD ["air", "-c", ".air.toml"]
