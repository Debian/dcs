// Generates self-signed TLS certificates to be used a DCS production
// installation. They have a 10 year expiration time and contain all the
// hostnames used in a production installation.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"
)

func generatecert() error {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %s", err)
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(10 * 365 * 24 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %s", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Debian Code Search"},
		},
		DNSNames: []string{
			"localhost",
			"monitoring.rackspace.zekjur.net",
			"int-dcsi-web.rackspace.zekjur.net",
			"int-dcsi-index-0.rackspace.zekjur.net",
			"int-dcsi-index-1.rackspace.zekjur.net",
			"int-dcsi-index-2.rackspace.zekjur.net",
			"int-dcsi-index-3.rackspace.zekjur.net",
			"int-dcsi-index-4.rackspace.zekjur.net",
			"int-dcsi-index-5.rackspace.zekjur.net",
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:     true,
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("Failed to create certificate: %s", err)
	}

	certOut, err := os.Create("prod-cert.pem")
	if err != nil {
		return fmt.Errorf("failed to open prod-cert.pem for writing: %s", err)
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	certOut.Close()

	keyOut, err := os.OpenFile("prod-key.pem", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open prod-key.pem for writing:", err)
	}
	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	keyOut.Close()
	return nil
}

func main() {
	if err := generatecert(); err != nil {
		log.Fatal(err)
	}
}
