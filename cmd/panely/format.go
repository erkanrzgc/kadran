package main

import (
	"fmt"
	"strings"
	"time"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// humanBytes, bayt sayısını okunabilir bir birime çevirir.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// humanDuration, süreyi en fazla iki birimle yazar.
//
// "3g 4sa" gibi bir çıktı, "3 gün 4 saat 17 dakika 3 saniye"den daha
// hızlı okunur ve çalışma süresi için o hassasiyet zaten gereksiz.
func humanDuration(d time.Duration) string {
	if d < time.Second {
		return "0sn"
	}

	units := []struct {
		size  time.Duration
		label string
	}{
		{24 * time.Hour, "g"},
		{time.Hour, "sa"},
		{time.Minute, "dk"},
		{time.Second, "sn"},
	}

	var parts []string
	for _, u := range units {
		if d < u.size {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d%s", d/u.size, u.label))
		d %= u.size
		if len(parts) == 2 {
			break
		}
	}
	return strings.Join(parts, " ")
}

// shortFingerprint, uzun SSH parmak izini listede okunabilir hale getirir.
//
// Tam değer kısaltılmadan denetim kaydında duruyor; `--json` ile görülebilir.
// Ekranda kısaltmak, satırların kaymasını ve asıl bilginin (eylem, hedef)
// ekrandan taşmasını önlüyor.
func shortFingerprint(fp string) string {
	const prefix = "SHA256:"
	body := strings.TrimPrefix(fp, prefix)
	if len(body) <= 16 {
		return fp
	}
	return prefix + body[:8] + "…" + body[len(body)-6:]
}

// describeActor, denetim listesi için aktörü tek bir hücreye sığdırır.
func describeActor(a *panelyv1.Actor) string {
	if a == nil {
		return "bilinmiyor"
	}
	if fp := a.GetSshKeyFingerprint(); fp != "" {
		return shortFingerprint(fp)
	}
	if label := a.GetLabel(); label != "" {
		return label
	}
	if origin := a.GetOrigin(); origin != "" {
		return origin
	}
	return "bilinmiyor"
}

// outcomeLabel, denetim sonucunu Türkçeleştirir.
func outcomeLabel(o panelyv1.AuditOutcome) string {
	switch o {
	case panelyv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS:
		return "BAŞARILI"
	case panelyv1.AuditOutcome_AUDIT_OUTCOME_FAILURE:
		return "BAŞARISIZ"
	case panelyv1.AuditOutcome_AUDIT_OUTCOME_DENIED:
		// Güvenlik modelinin devreye girdiği durum: ayrıca izlenir.
		return "REDDEDİLDİ"
	case panelyv1.AuditOutcome_AUDIT_OUTCOME_UNSPECIFIED:
		return "belirsiz"
	default:
		return "belirsiz"
	}
}

// chainStatusLabel, zincir durumunu ekrana yazılacak biçime çevirir.
//
// UNREACHABLE'ın "GEÇERSİZ" değil "DOĞRULANAMADI" olarak görünmesi
// kasıtlıdır: birincisi kurcalama şüphesi, ikincisi işletim sorunudur ve
// operatörün tepkisi tamamen farklıdır.
func chainStatusLabel(s panelyv1.ChainStatus) string {
	switch s {
	case panelyv1.ChainStatus_CHAIN_STATUS_VALID:
		return "GEÇERLİ"
	case panelyv1.ChainStatus_CHAIN_STATUS_INVALID:
		return "GEÇERSİZ"
	case panelyv1.ChainStatus_CHAIN_STATUS_UNREACHABLE:
		return "DOĞRULANAMADI"
	case panelyv1.ChainStatus_CHAIN_STATUS_UNSPECIFIED:
		return "BİLİNMİYOR"
	default:
		return "BİLİNMİYOR"
	}
}
