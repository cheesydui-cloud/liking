package certs

import (
	"fmt"
	"net"
	"strings"
	"unicode"
)

func SplitNames(s string) []string {
	s = strings.ReplaceAll(s, ";", ",")
	s = strings.ReplaceAll(s, "\n", ",")
	s = strings.ReplaceAll(s, "\t", ",")
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.TrimSuffix(p, ".")
		p = strings.ToLower(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func JoinNames(names []string) string {
	return strings.Join(names, ", ")
}

func DNS01Name(domain string) string {
	d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	d = strings.TrimPrefix(d, "*.")
	return "_acme-challenge." + d
}

func ZoneCandidates(name string) []string {
	n := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	n = strings.TrimPrefix(n, "*.")
	n = strings.TrimPrefix(n, "_acme-challenge.")
	labels := strings.Split(n, ".")
	if len(labels) < 2 {
		return nil
	}
	var out []string
	for i := 0; i <= len(labels)-2; i++ {
		out = append(out, strings.Join(labels[i:], "."))
	}
	return out
}

func NormalizeDNS(names []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(n)), ".")
		if n == "" {
			continue
		}
		if err := checkDNSName(n); err != nil {
			return nil, err
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("请填写域名")
	}
	return out, nil
}

func checkDNSName(n string) error {
	if strings.ContainsAny(n, " /:\\") || strings.Contains(n, "..") {
		return fmt.Errorf("域名无效：%s", n)
	}
	if ip := net.ParseIP(n); ip != nil {
		return fmt.Errorf("Let's Encrypt DNS 签发不支持 IP：%s", n)
	}
	rest := n
	if strings.HasPrefix(n, "*.") {
		rest = n[2:]
		if rest == "" || strings.Contains(rest, "*") {
			return fmt.Errorf("泛域名无效：%s", n)
		}
	} else if strings.Contains(n, "*") {
		return fmt.Errorf("域名无效：%s", n)
	}
	if !strings.Contains(rest, ".") {
		return fmt.Errorf("域名无效：%s", n)
	}
	return nil
}
