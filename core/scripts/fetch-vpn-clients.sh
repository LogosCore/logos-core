#!/bin/sh
# fetch-vpn-clients.sh <dir>: puts the logos-vpn release that
# vpn-client.version names into dir, for core to serve (VPN_CLIENT_DIR,
# GET /api/v1/vpn/client/{os}-{arch}; docs: contracts/vpn-front). Every
# binary is checked against the release's SHA256SUMS, and all six must be
# there: any failure fails the image build. A new logos-vpn release is one
# line in vpn-client.version.
set -eu
dir=$1
version=$(tr -d ' \r\n' < "$(dirname "$0")/../vpn-client.version")
base="https://github.com/LogosCore/logos-vpn/releases/download/$version"
mkdir -p "$dir"
cd "$dir"
wget -q -O SHA256SUMS "$base/SHA256SUMS" || { echo "logos-vpn $version: no release at $base" >&2; exit 1; }
for p in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64 windows-arm64; do
  f="logos-vpn-$p"
  case $p in windows-*) f="$f.exe" ;; esac
  grep -q "  $f\$" SHA256SUMS || { echo "logos-vpn $version: no $f in its SHA256SUMS" >&2; exit 1; }
  wget -q -O "$f" "$base/$f" || { echo "logos-vpn $version: $f does not download" >&2; exit 1; }
done
sha256sum -c SHA256SUMS
chmod 0644 logos-vpn-*
rm SHA256SUMS
echo "logos-vpn $version: $(ls | wc -l) binaries"
