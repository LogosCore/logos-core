# logos-nginx: core's front, its routes kept with core's code (docs:
# deployment, "Core's front"). The official nginx image renders
# templates/default.conf.template from the environment at start;
# 25-logos-front.sh picks the plain HTTP server's mode and the certificate.
FROM nginx:1.29.5-alpine

RUN apk add --no-cache openssl \
    && mkdir -p /etc/nginx/logos-extra /etc/nginx/logos-certs /etc/nginx/certs \
    && rm -f /etc/nginx/conf.d/default.conf

COPY templates/ /etc/nginx/templates/
COPY locations.conf /etc/nginx/logos-locations.conf
COPY 25-logos-front.sh /docker-entrypoint.d/25-logos-front.sh

# Where core, the frontend and Hocuspocus listen, host:port; the name the
# servers answer to ("_": any); the plain HTTP server's mode (serve or
# redirect). Certificates: mount tls.crt and tls.key at /etc/nginx/certs
# (or name them: LOGOS_TLS_CERT, LOGOS_TLS_KEY), or keep the one the image
# makes with a volume on /etc/nginx/logos-certs. Extra server-level
# configuration: *.conf files in /etc/nginx/logos-extra.
ENV LOGOS_BACKEND=core:8002 \
    LOGOS_FRONTEND=frontend:80 \
    LOGOS_HOCUSPOCUS=hocuspocus:1234 \
    LOGOS_SERVER_NAME=_ \
    LOGOS_HTTP=serve \
    NGINX_ENVSUBST_FILTER=^LOGOS_

EXPOSE 80 443
