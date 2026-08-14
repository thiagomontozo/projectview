#!/bin/sh
# Bootstraps a TLS certificate before starting nginx.
#
# In production you mount your real certificate into /etc/nginx/certs as
# fullchain.pem + privkey.pem (see docker-compose.yml). When that directory is
# empty - a fresh clone, a demo, a CI run - a self-signed certificate is
# generated so the stack still comes up over HTTPS instead of failing to boot.
set -e

CERT_DIR=/etc/nginx/certs
CRT="$CERT_DIR/fullchain.pem"
KEY="$CERT_DIR/privkey.pem"
CN="${TLS_COMMON_NAME:-localhost}"

mkdir -p "$CERT_DIR"

if [ ! -f "$CRT" ] || [ ! -f "$KEY" ]; then
    echo "[proxy] No certificate found in $CERT_DIR - generating a self-signed one for CN=$CN."
    echo "[proxy] Browsers will warn about it. Mount a real fullchain.pem/privkey.pem for production."
    gencert "$CRT" "$KEY" "$CN" "${TLS_SELF_SIGNED_DAYS:-825}"
else
    echo "[proxy] Using the certificate found in $CERT_DIR."
fi

exec "$@"
