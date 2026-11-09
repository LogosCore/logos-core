#!/bin/sh
# logos-nginx's start: the plain HTTP server's mode, and the certificate.
# Runs from the official image's /docker-entrypoint.d before nginx starts.
set -eu

case "${LOGOS_HTTP:-serve}" in
  serve) echo 'include /etc/nginx/logos-locations.conf;' > /etc/nginx/logos-http.conf ;;
  redirect) echo 'location / { return 301 https://$host$request_uri; }' > /etc/nginx/logos-http.conf ;;
  *) echo "logos-nginx: LOGOS_HTTP is serve or redirect, not ${LOGOS_HTTP}" >&2; exit 1 ;;
esac

# The certificate: the one mounted (LOGOS_TLS_CERT, LOGOS_TLS_KEY), else
# the one made before (a volume on /etc/nginx/logos-certs keeps it), else
# a new self-signed one, for LOGOS_SERVER_NAME and localhost, ten years.
dir=/etc/nginx/logos-certs
cert=${LOGOS_TLS_CERT:-/etc/nginx/certs/tls.crt}
key=${LOGOS_TLS_KEY:-/etc/nginx/certs/tls.key}
mkdir -p "$dir"
if [ -f "$cert" ] && [ -f "$key" ]; then
  ln -sf "$cert" "$dir/tls.crt"
  ln -sf "$key" "$dir/tls.key"
  echo "logos-nginx: certificate $cert"
elif [ -f "$dir/tls.crt" ] && [ -f "$dir/tls.key" ]; then
  echo "logos-nginx: the self-signed certificate made before ($dir)"
else
  name=${LOGOS_SERVER_NAME:-_}
  [ "$name" = "_" ] && name=logos
  san="DNS:localhost,IP:127.0.0.1"
  for n in $name; do
    case "$n" in
      *[!0-9.]*) san="DNS:$n,$san" ;;
      *) san="IP:$n,$san" ;;
    esac
  done
  first=${name%% *}
  openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 3650 -subj "/CN=$first" \
    -addext "subjectAltName=$san" -keyout "$dir/tls.key" -out "$dir/tls.crt" 2>/dev/null
  chmod 600 "$dir/tls.key"
  echo "logos-nginx: made a self-signed certificate for $san ($dir; keep it with a volume there)"
fi

# Its fingerprint: what modules name in LOGOS_CORE_CERT_SHA256.
fp=$(openssl x509 -noout -fingerprint -sha256 -in "$dir/tls.crt" | sed 's/^.*=//')
echo "logos-nginx: certificate SHA-256 $fp (modules: LOGOS_CORE_CERT_SHA256=$fp)"
