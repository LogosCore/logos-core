ARG NODE_VERSION=22
ARG NGINX_VERSION=1.27
# Where the app comes from: built here from source (builder, the default),
# or by CI before the image, with its node_modules cache (prebuilt:
# publish-frontend.yml builds it into ci-dist/). Built here, a new node
# image meant a fresh npm ci in CI: 156s of round 37's 281s publish.
ARG DIST_FROM=builder

FROM node:${NODE_VERSION}-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json ./
# Before `npm ci`: its postinstall runs patch-package, which applies patches/
# to node_modules. Without them in the image yet it prints "No patch files
# found" and exits 0, so the build succeeded and shipped unpatched packages.
COPY patches ./patches
RUN npm ci

COPY . .

RUN npm run build

# The app CI built, in ci-dist/. The directory is committed (.keep) so a
# builder that builds every stage, Docker's classic one, finds it; BuildKit
# builds only the stage DIST_FROM names.
FROM scratch AS prebuilt
COPY ci-dist/ /app/dist/

FROM ${DIST_FROM} AS dist


FROM nginx:${NGINX_VERSION}-alpine

RUN rm /etc/nginx/conf.d/default.conf

COPY nginx.conf /etc/nginx/conf.d/default.conf

COPY --from=dist /app/dist /usr/share/nginx/html

EXPOSE 80
