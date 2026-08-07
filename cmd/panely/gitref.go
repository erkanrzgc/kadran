package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ── Dal → commit çözümü (git smart HTTP) ─────────────────────────────
//
// `git` ikilisine SHELL ÇAĞRISI YAPILMIYOR. İki sebep:
//
//  1. Bağımlılık: iş istasyonunda git kurulu olmayabilir ve Electron
//     kabuğu bu ikiliyi sidecar olarak çalıştırıyor. Kurulum şartını
//     "tek binary" vaadinin üstüne eklemek istemiyoruz.
//  2. Argüman güvenliği: bir alt sürece dal adı geçirmek, projenin
//     baştan beri kaçındığı sınıfı (serbest argv) istemci tarafında geri
//     getirirdi. Burada dal adı bir SORGU PARAMETRESİ bile değil; yalnızca
//     yanıtta ARANAN bir dizedir.
//
// Kullanılan uç, git'in "smart HTTP" keşif isteğidir:
//
//	GET https://<host>/<owner>/<repo>.git/info/refs?service=git-upload-pack
//
// Yanıt pkt-line akışıdır: her kaydın başında 4 haneli onaltılık uzunluk
// (kendisi dahil), "0000" akış sonu işaretidir.

const (
	// refDiscoveryTimeout, dal çözümü için üst sınır.
	//
	// Bu sınır DERLEMEYİ kapsamıyor: yalnızca tek bir HTTP isteği. K-044
	// buraya uygulanmaz — sınırlanan şey akış değil, bir istek/yanıt turu.
	refDiscoveryTimeout = 20 * time.Second

	pktLenSize   = 4
	maxRefsBytes = 8 << 20
)

// resolveRemoteBranch, uzak deponun bir dalının işaret ettiği commit'i
// döner.
func resolveRemoteBranch(ctx context.Context, host, owner, repo, branch string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, refDiscoveryTimeout)
	defer cancel()

	// URL parçalardan KURULUYOR, bir dizeden alınmıyor: şema sabit https,
	// yol bileşenleri kaçışlanıyor ve kullanıcı bilgisi temsil edilemiyor.
	endpoint := (&url.URL{
		Scheme:   "https",
		Host:     host,
		Path:     "/" + owner + "/" + repo + ".git/info/refs",
		RawQuery: url.Values{"service": {"git-upload-pack"}}.Encode(),
	}).String()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("dal çözümü isteği kurulamadı: %w", err)
	}
	req.Header.Set("User-Agent", "git/2.0 (panely)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("depo sorgulanamadı (%s/%s/%s): %w", host, owner, repo, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf(
			"depo sorgusu %s döndü (%s/%s/%s) — depo özel veya adı yanlış olabilir",
			resp.Status, host, owner, repo)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRefsBytes))
	if err != nil {
		return "", fmt.Errorf("dal listesi okunamadı: %w", err)
	}

	sha, err := findRef(body, branch)
	if err != nil {
		return "", fmt.Errorf("%w (%s/%s/%s)", err, host, owner, repo)
	}
	return sha, nil
}

// findRef, pkt-line akışında bir dalın sha'sını arar.
//
// Hem `refs/heads/<dal>` hem `refs/tags/<dal>` denenir; etiketten
// çözülen sha da tam 40 hanedir ve donmuştur, yani tekrarlanabilirlik
// korunur. Etiket için `^{}` sonekli satır (dereferans edilmiş nesne)
// varsa O tercih edilir — annotated etiketin kendi nesnesi değil,
// gösterdiği commit derlenmeli.
func findRef(body []byte, branch string) (string, error) {
	var (
		heads    string
		tag      string
		tagPeel  string
		wantHead = "refs/heads/" + branch
		wantTag  = "refs/tags/" + branch
	)

	for rest := body; len(rest) >= pktLenSize; {
		n, err := strconv.ParseUint(string(rest[:pktLenSize]), 16, 32)
		if err != nil {
			return "", errors.New("depo yanıtı pkt-line biçiminde değil")
		}
		switch {
		case n == 0: // akış sonu işareti
			rest = rest[pktLenSize:]
			continue
		case n < pktLenSize || int(n) > len(rest):
			return "", errors.New("depo yanıtında bozuk pkt-line uzunluğu")
		}

		line := strings.TrimRight(string(rest[pktLenSize:n]), "\n")
		rest = rest[n:]

		// "<sha> <ref>" — ilk ref satırında NUL'dan sonra yetenekler gelir.
		sha, ref, ok := strings.Cut(line, " ")
		if !ok || len(sha) != 40 {
			continue
		}
		ref, _, _ = strings.Cut(ref, "\x00")

		switch ref {
		case wantHead:
			heads = sha
		case wantTag:
			tag = sha
		case wantTag + "^{}":
			tagPeel = sha
		}
	}

	switch {
	case heads != "":
		return heads, nil
	case tagPeel != "":
		return tagPeel, nil
	case tag != "":
		return tag, nil
	default:
		return "", fmt.Errorf("dal veya etiket bulunamadı: %q", branch)
	}
}
