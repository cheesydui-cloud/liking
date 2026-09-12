package certs

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

func ParseMeta(certPEM string) (domains []string, notAfter time.Time, err error) {
	rest := []byte(certPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("证书 PEM 无法解析")
		}
		seen := map[string]bool{}
		add := func(s string) {
			s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
			if s == "" || seen[s] {
				return
			}
			seen[s] = true
			domains = append(domains, s)
		}
		if c.Subject.CommonName != "" {
			add(c.Subject.CommonName)
		}
		for _, n := range c.DNSNames {
			add(n)
		}
		for _, ip := range c.IPAddresses {
			add(ip.String())
		}
		return domains, c.NotAfter.UTC(), nil
	}
	return nil, time.Time{}, errors.New("PEM 中没有证书")
}

func ParsePrivateKey(keyPEM string) (crypto.PrivateKey, error) {
	rest := []byte(keyPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if k, err := parseKeyBlock(block.Bytes); err == nil {
			return k, nil
		}
	}
	return nil, errors.New("私钥 PEM 无法解析")
}

func parseKeyBlock(b []byte) (crypto.PrivateKey, error) {
	if k, err := x509.ParsePKCS8PrivateKey(b); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS1PrivateKey(b); err == nil {
		return k, nil
	}
	if k, err := x509.ParseECPrivateKey(b); err == nil {
		return k, nil
	}
	return nil, errors.New("unknown key")
}

func KeyMatchesCert(certPEM string, key crypto.PrivateKey) error {
	rest := []byte(certPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("证书 PEM 无法解析")
		}
		switch k := key.(type) {
		case *rsa.PrivateKey:
			pub, ok := c.PublicKey.(*rsa.PublicKey)
			if !ok || k.PublicKey.N.Cmp(pub.N) != 0 {
				return errors.New("证书和私钥不匹配")
			}
		case *ecdsa.PrivateKey:
			pub, ok := c.PublicKey.(*ecdsa.PublicKey)
			if !ok || pub.X.Cmp(k.X) != 0 || pub.Y.Cmp(k.Y) != 0 {
				return errors.New("证书和私钥不匹配")
			}
		default:
			return errors.New("不支持的私钥类型")
		}
		return nil
	}
	return errors.New("PEM 中没有证书")
}

func EncodePEMCerts(ders [][]byte) string {
	var b []byte
	for _, der := range ders {
		b = append(b, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	return string(b)
}

func EncodePKCS8Key(key crypto.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

func LoadAccountKey(pemStr string) (*ecdsa.PrivateKey, error) {
	if pemStr == "" {
		return nil, errors.New("empty")
	}
	k, err := ParsePrivateKey(pemStr)
	if err != nil {
		return nil, err
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("ACME 账户密钥必须是 ECDSA")
	}
	return ek, nil
}
