# Webmail (Vite/React) → static files served by nginx, which does the SAME-ORIGIN proxy to the Go API.
# The API handles /api (control plane) and brokers JMAP on /jmap,/auth,/.well-known, so nginx
# NEVER talks to Stalwart directly -- everything goes to the `api` service.

FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci --legacy-peer-deps || npm install --legacy-peer-deps
COPY . .
# production web build (same origin). `build` = tsc -b && vite build → ./dist
RUN npm run build

FROM nginx:1.27-alpine
# The API host is configurable (env API_UPSTREAM) and resolved at runtime (the service may start later).
ENV API_UPSTREAM=srv-captain--api:8000
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
