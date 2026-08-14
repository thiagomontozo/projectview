FROM node:20-alpine AS build
WORKDIR /app

# Optional extra root CAs (see ca/README.md). Node uses its own bundled trust
# store, so NODE_EXTRA_CA_CERTS is how npm learns to trust a TLS-intercepting
# proxy. The file is always created; when empty, Node just ignores it.
COPY --from=ca . /tmp/extra-ca/
RUN mkdir -p /usr/local/share/ca-certificates \
    && cat /tmp/extra-ca/*.crt > /usr/local/share/ca-certificates/extra-ca.crt 2>/dev/null || true \
    && touch /usr/local/share/ca-certificates/extra-ca.crt
ENV NODE_EXTRA_CA_CERTS=/usr/local/share/ca-certificates/extra-ca.crt

COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
