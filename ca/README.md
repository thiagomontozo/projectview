# Extra root CAs for the build

Some networks intercept TLS — corporate inspection proxies, and several
antivirus products with an "HTTPS scanning" feature. The host usually trusts
the interceptor's root certificate, but **build containers do not**, so
`go mod download` and `npm ci` fail with:

```
tls: failed to verify certificate: x509: certificate signed by unknown authority
```

Drop the interceptor's root certificate here as a PEM file ending in `.crt`
and rebuild. The backend and frontend build stages pick up everything in this
directory and add it to the trust store they use to fetch dependencies. This
directory is wired in as a Docker
[additional build context](https://docs.docker.com/reference/compose-file/build/#additional_contexts)
named `ca`, so it stays out of the individual build contexts.

Nothing here is required: with no `.crt` file, builds run against the default
trust store exactly as before. (Node prints one
`ignoring extra certs` warning in that case — harmless.)

Certificates are gitignored, so nothing you put here is ever committed.

## Exporting the certificate

**Linux/macOS** — take the last certificate of the presented chain:

```bash
openssl s_client -showcerts -connect proxy.golang.org:443 </dev/null 2>/dev/null \
  | awk '/BEGIN CERT/,/END CERT/' > ca/tls-inspection-root.crt
```

**Windows (PowerShell)**:

```powershell
$tcp = New-Object Net.Sockets.TcpClient("proxy.golang.org", 443)
$ssl = New-Object Net.Security.SslStream($tcp.GetStream(), $false, { $true })
$ssl.AuthenticateAsClient("proxy.golang.org")
$leaf = New-Object Security.Cryptography.X509Certificates.X509Certificate2($ssl.RemoteCertificate)
$chain = New-Object Security.Cryptography.X509Certificates.X509Chain
$null = $chain.Build($leaf)
$root = $chain.ChainElements[$chain.ChainElements.Count - 1].Certificate
$b64 = [Convert]::ToBase64String($root.RawData, 'InsertLineBreaks')
"-----BEGIN CERTIFICATE-----`n$b64`n-----END CERTIFICATE-----" |
    Set-Content ca\tls-inspection-root.crt -Encoding ascii
```

If the issuer looks like your antivirus rather than your employer, turning off
its HTTPS scanning is the cleaner fix.
