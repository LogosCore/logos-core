ARG NODE_VERSION=22
ARG NGINX_VERSION=1.27

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


FROM nginx:${NGINX_VERSION}-alpine

RUN rm /etc/nginx/conf.d/default.conf

COPY nginx.conf /etc/nginx/conf.d/default.conf

COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80
