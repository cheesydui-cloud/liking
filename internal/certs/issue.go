package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
)

var dnsPropagate = 12 * time.Second

type IssueRequest struct {
	Domains       []string
	Email         string
	AccountKeyPEM string
	CFToken       string
	DirectoryURL  string
	CFAPI         string
	HTTPClient    *http.Client
}

type IssueResult struct {
	CertPEM       string
	KeyPEM        string
	Domains       string
	ExpiresAt     int64
	AccountKeyPEM string
}

func Issue(ctx context.Context, req IssueRequest) (*IssueResult, error) {
	domains, err := NormalizeDNS(req.Domains)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.CFToken) == "" {
		return nil, fmt.Errorf("请先保存 Cloudflare API Token")
	}
	accountKey, err := LoadAccountKey(req.AccountKeyPEM)
	if err != nil {
		accountKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
	}
	acctPEM, err := EncodePKCS8Key(accountKey)
	if err != nil {
		return nil, err
	}
	out := &IssueResult{AccountKeyPEM: acctPEM, Domains: JoinNames(domains)}

	httpc := req.HTTPClient
	if httpc == nil {
		httpc = &http.Client{Timeout: 30 * time.Second}
	}
	client := &acme.Client{
		Key:          accountKey,
		HTTPClient:   httpc,
		DirectoryURL: req.DirectoryURL,
		UserAgent:    "liking",
	}
	acct := &acme.Account{}
	if email := strings.TrimSpace(req.Email); email != "" {
		acct.Contact = []string{"mailto:" + email}
	}
	_, err = client.Register(ctx, acct, func(string) bool { return true })
	if errors.Is(err, acme.ErrAccountAlreadyExists) {
		_, err = client.GetReg(ctx, "")
	}
	if err != nil {
		return out, fmt.Errorf("注册 Let's Encrypt 账户失败：%w", err)
	}

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domains...))
	if err != nil {
		return out, fmt.Errorf("创建订单失败：%w", err)
	}

	cf := &Cloudflare{Token: req.CFToken, HTTP: httpc, API: req.CFAPI}
	type placed struct {
		name, content string
		chal          *acme.Challenge
		authzURL      string
	}
	var recs []placed
	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, p := range recs {
			_ = cf.DeleteTXT(cleanCtx, p.name, p.content)
		}
	}()

	for _, au := range order.AuthzURLs {
		az, err := client.GetAuthorization(ctx, au)
		if err != nil {
			return out, fmt.Errorf("读取授权失败：%w", err)
		}
		if az.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, ch := range az.Challenges {
			if ch != nil && ch.Type == "dns-01" {
				chal = ch
				break
			}
		}
		if chal == nil {
			return out, fmt.Errorf("Let's Encrypt 没有提供 DNS 验证：%s", az.Identifier.Value)
		}
		val, err := client.DNS01ChallengeRecord(chal.Token)
		if err != nil {
			return out, err
		}
		name := DNS01Name(az.Identifier.Value)
		if err := cf.Present(ctx, name, val); err != nil {
			return out, err
		}
		recs = append(recs, placed{name: name, content: val, chal: chal, authzURL: au})
	}

	if wait := dnsPropagate; wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return out, ctx.Err()
		case <-t.C:
		}
	}

	for _, p := range recs {
		if _, err := client.Accept(ctx, p.chal); err != nil {
			return out, fmt.Errorf("提交 DNS 验证失败：%w", err)
		}
		if _, err := client.WaitAuthorization(ctx, p.authzURL); err != nil {
			return out, fmt.Errorf("DNS 验证未通过：%w", err)
		}
	}

	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return out, fmt.Errorf("等待签发失败：%w", err)
	}

	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return out, err
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: domains[0]},
		DNSNames: domains,
	}, certKey)
	if err != nil {
		return out, err
	}

	var der [][]byte
	if order.Status == acme.StatusValid && order.CertURL != "" {
		der, err = client.FetchCert(ctx, order.CertURL, true)
	} else {
		der, _, err = client.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	}
	if err != nil {
		return out, fmt.Errorf("下载证书失败：%w", err)
	}
	if len(der) == 0 {
		return out, fmt.Errorf("Let's Encrypt 没有返回证书")
	}
	leaf, err := x509.ParseCertificate(der[0])
	if err != nil {
		return out, err
	}
	keyPEM, err := EncodePKCS8Key(certKey)
	if err != nil {
		return out, err
	}
	out.CertPEM = EncodePEMCerts(der)
	out.KeyPEM = keyPEM
	out.ExpiresAt = leaf.NotAfter.UTC().Unix()
	return out, nil
}
