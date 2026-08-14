# TLS certificates

Drop your certificate here as two files:

- `fullchain.pem` — the server certificate followed by the intermediate chain
- `privkey.pem` — the matching private key

They are mounted into the `proxy` container at `/etc/nginx/certs`.

If this directory has no certificate when the stack starts, the proxy
generates a **self-signed** one for `TLS_COMMON_NAME` (default `localhost`)
so the stack still comes up over HTTPS. Browsers will show a warning for it —
that is expected, and it is not suitable for production.

Certificates and keys are gitignored (`*.pem`, `*.key`, `*.crt`); never commit
them.

## Let's Encrypt

The proxy keeps `/.well-known/acme-challenge/` reachable over plain HTTP
(served from `/var/www/certbot`), so an http-01 challenge works. Point
certbot's webroot at that path, then copy the issued `fullchain.pem` and
`privkey.pem` into this directory and reload the proxy:

```bash
docker compose exec proxy nginx -s reload
```
