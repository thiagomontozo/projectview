// Command gencert writes a self-signed TLS certificate/key pair.
//
// It exists so the proxy image needs no package manager at build time: the
// nginx alpine base ships without the openssl CLI, and installing it requires
// network access to the Alpine mirrors, which fails on networks that
// intercept TLS. This is a ~2 MB static binary built from the standard
// library instead.
//
// It is only a fallback for local runs and demos - production should mount a
// real certificate into /etc/nginx/certs (see ../certs/README.md).
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"os"
	"strconv"
	"time"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[gencert] ")

	if len(os.Args) < 4 {
		log.Fatal("usage: gencert <cert.pem> <key.pem> <common-name> [days]")
	}
	certPath, keyPath, commonName := os.Args[1], os.Args[2], os.Args[3]

	days := 825
	if len(os.Args) > 4 {
		if d, err := strconv.Atoi(os.Args[4]); err == nil && d > 0 {
			days = d
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("could not generate key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		log.Fatalf("could not generate serial number: %v", err)
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"ProjectView (self-signed)"},
		},
		NotBefore:             now.Add(-time.Hour), // tolerate host clock skew
		NotAfter:              now.AddDate(0, 0, days),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames(commonName),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		log.Fatalf("could not create certificate: %v", err)
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		log.Fatalf("could not write %s: %v", certPath, err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		log.Fatalf("could not marshal key: %v", err)
	}
	if err := writePEM(keyPath, "PRIVATE KEY", keyDER, 0o600); err != nil {
		log.Fatalf("could not write %s: %v", keyPath, err)
	}

	log.Printf("wrote a self-signed certificate for %q, valid for %d days", commonName, days)
}

// dnsNames always includes localhost, so a stack started with a real
// TLS_COMMON_NAME is still reachable at https://localhost during development.
func dnsNames(commonName string) []string {
	names := []string{commonName}
	if commonName != "localhost" {
		names = append(names, "localhost")
	}
	return names
}

func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		return err
	}
	return f.Close()
}
