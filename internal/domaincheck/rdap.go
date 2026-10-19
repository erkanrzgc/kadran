package domaincheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/publicsuffix"
)

// IANABootstrapURL, IANA'nın RDAP önyükleme dosyası (RFC 9224). Üçüncü
// taraf yönlendirici (rdap.org) KULLANILMAZ: domain adı yalnız kayıt
// otoritesine gider (K-151).
const IANABootstrapURL = "https://data.iana.org/rdap/dns.json"

// RDAPTimeout, tek RDAP isteğinin süre sınırı.
const RDAPTimeout = 5 * time.Second

// rdapMaxBody, okunacak en büyük RDAP cevabı.
const rdapMaxBody = 1 << 20

// maxRegistrarLen, basılacak firma adının en uzun hâli (rune).
const maxRegistrarLen = 100

// RDAPClient, yalnız https yönlendirmesi izleyen HTTP istemcisi.
func RDAPClient() *http.Client {
	return &http.Client{
		Timeout: RDAPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("rdap: refusing a redirect that is not https")
			}
			if len(via) >= 5 {
				return errors.New("rdap: too many redirects")
			}
			return nil
		},
	}
}

// LookupRegistrar, domainin akredite kayıt firmasını RDAP'tan okur. RDAP'ı
// olmayan uzantıda (ör. .tr) ya da registrar varlığı yoksa "" ve nil döner.
// Bayi (Vercel gibi) değil, akredite kayıt firması döner: erkanrzgc.dev
// Vercel'den alındı, RDAP Name.com dedi (15 Ekim 2026).
func LookupRegistrar(ctx context.Context, hc *http.Client, bootstrapURL, domain string) (string, error) {
	reg, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return "", err
	}
	tld := reg[strings.LastIndex(reg, ".")+1:]

	var boot struct {
		Services [][][]string `json:"services"`
	}
	if err := getJSON(ctx, hc, bootstrapURL, &boot); err != nil {
		return "", err
	}
	base := ""
	for _, svc := range boot.Services {
		if len(svc) != 2 || !slices.Contains(svc[0], tld) {
			continue
		}
		for _, u := range svc[1] {
			if strings.HasPrefix(u, "https://") {
				base = u
				break
			}
		}
	}
	if base == "" {
		return "", nil
	}

	var dom struct {
		Entities []rdapEntity `json:"entities"`
	}
	if err := getJSON(ctx, hc, strings.TrimSuffix(base, "/")+"/domain/"+url.PathEscape(reg), &dom); err != nil {
		return "", err
	}
	for _, e := range dom.Entities {
		if slices.Contains(e.Roles, "registrar") {
			return cleanText(e.fn()), nil
		}
	}
	return "", nil
}

type rdapEntity struct {
	Roles []string          `json:"roles"`
	VCard []json.RawMessage `json:"vcardArray"`
}

// fn, vCard'ın "fn" (görünen ad) alanı: ["vcard", [[ad, {}, tip, değer], …]].
func (e rdapEntity) fn() string {
	if len(e.VCard) < 2 {
		return ""
	}
	var props [][]json.RawMessage
	if json.Unmarshal(e.VCard[1], &props) != nil {
		return ""
	}
	for _, p := range props {
		if len(p) < 4 {
			continue
		}
		var name, val string
		if json.Unmarshal(p[0], &name) != nil || name != "fn" {
			continue
		}
		if json.Unmarshal(p[3], &val) == nil {
			return val
		}
	}
	return ""
}

func getJSON(ctx context.Context, hc *http.Client, u string, v any) error {
	if !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("rdap: %s is not https", u)
	}
	ctx, cancel := context.WithTimeout(ctx, RDAPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rdap: %s answered %s", u, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, rdapMaxBody)).Decode(v)
}

// cleanText, güvenilmez metni basılabilir yapar: kontrol ve kaçış
// karakterleri atılır, uzunluk sınırlanır.
func cleanText(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= maxRegistrarLen {
			break
		}
		if unicode.IsPrint(r) {
			b.WriteRune(r)
			n++
		}
	}
	return strings.TrimSpace(b.String())
}
