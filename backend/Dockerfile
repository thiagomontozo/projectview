FROM golang:1.24-alpine AS build
WORKDIR /app

# Optional extra root CAs (see ca/README.md). Networks that intercept TLS
# break module downloads unless their root is trusted inside the build
# container. With no certificate provided this is a no-op.
COPY --from=ca . /tmp/extra-ca/
RUN cat /tmp/extra-ca/*.crt >> /etc/ssl/certs/ca-certificates.crt 2>/dev/null || true

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /projectview-server ./cmd/server

FROM alpine:3.20
WORKDIR /app
# The root CA bundle is copied from the build image rather than installed with
# apk: the runtime layer then needs no package manager and no network access.
# Timezone data is embedded in the binary itself (see the time/tzdata import
# in cmd/server/main.go).
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /projectview-server ./projectview-server
ENV NODE_ENV=production
EXPOSE 4000
CMD ["./projectview-server"]
