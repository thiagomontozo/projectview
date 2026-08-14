# Builds the self-signed-certificate helper as a static binary, so the final
# image needs no package manager (the nginx alpine base has no openssl CLI).
FROM golang:1.24-alpine AS gencert
WORKDIR /src
COPY gencert/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gencert .

FROM nginx:1.27-alpine

COPY --from=gencert /gencert /usr/local/bin/gencert
COPY nginx.conf /etc/nginx/nginx.conf
COPY docker-entrypoint.sh /usr/local/bin/proxy-entrypoint.sh
RUN chmod +x /usr/local/bin/proxy-entrypoint.sh

EXPOSE 80 443

# Probes the plain-HTTP health endpoint, which is served before the HTTPS
# redirect, so the check does not depend on TLS.
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 \
    CMD wget --no-verbose --tries=1 --spider http://localhost/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/proxy-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
