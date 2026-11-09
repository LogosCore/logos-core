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

# Whether clients pin it (LOGOS_TLS_PIN): auto, the default, when the
# image's certificate authorities do not verify it (a self-signed one);
# always (an authority the operators' machines do not trust either); never.
# Pinned, the HTTPS server tells core its fingerprint and its public key's
# pin (X-Logos-Cert-SHA256, X-Logos-Cert-Pin, on /api/), and Connect a
# machine's lines carry them: curl --pinnedpubkey, logos-vpn -cert-sha256.
pin=0
case "${LOGOS_TLS_PIN:-auto}" in
  always) pin=1 ;;
  never) ;;
  auto) openssl verify -CAfile /etc/ssl/certs/ca-certificates.crt -untrusted "$dir/tls.crt" "$dir/tls.crt" >/dev/null 2>&1 || pin=1 ;;
  *) echo "logos-nginx: LOGOS_TLS_PIN is auto, always or never, not ${LOGOS_TLS_PIN}" >&2; exit 1 ;;
esac
spki=""
if [ "$pin" = 1 ]; then
  spki=sha256//$(openssl x509 -noout -pubkey -in "$dir/tls.crt" | openssl pkey -pubin -outform DER | openssl dgst -sha256 -binary | openssl base64 -A)
  echo "logos-nginx: the certificate is pinned by clients (public key $spki)"
else
  fp=""
fi
cat > /etc/nginx/conf.d/logos-front-cert.conf <<EOF
# 25-logos-front.sh: the certificate clients pin, on HTTPS alone; empty
# when they need not.
map \$scheme \$logos_cert_sha256 { https "$fp"; default ""; }
map \$scheme \$logos_cert_pin { https "$spki"; default ""; }
EOF
