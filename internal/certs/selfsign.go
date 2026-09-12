package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"time"
)

const SelfSignYears = 5

func SelfSign(name string, names []string) (certPEM, keyPEM string, expiresAt int64, err error) {
	if len(names) == 0 && name != "" {
		names = []string{name}
	}
	if len(names) == 0 {
		return "", "", 0, fmt.Errorf("请填写名称或域名")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", 0, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", 0, err
	}
	now := time.Now().UTC().Add(-time.Minute)
	notAfter := now.AddDate(SelfSignYears, 0, 0)
	cn := names[0]
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"liking"}},
		NotBefore:             now,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
			continue
		}
		tpl.DNSNames = append(tpl.DNSNames, n)
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return "", "", 0, err
	}
	certPEM = EncodePEMCerts([][]byte{der})
	keyPEM, err = EncodePKCS8Key(key)
	if err != nil {
		return "", "", 0, err
	}
	return certPEM, keyPEM, notAfter.Unix(), nil
}
