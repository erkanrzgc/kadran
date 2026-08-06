package dockerdrv

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// LogFrame başlığının çözümü. ÖLÇÜLDÜ, varsayılmadı — gerçek daemon'dan
// alınan ilk 24 bayt:
//
//	01 00 00 00  00 00 00 0d  "cikti-satiri\n"   (13 bayt, stdout)
//	02 00 00 00  00 00 00 0c  "hata-satiri\n"    (12 bayt, stderr)
//
// Yani: bayt 0 akış türü, bayt 1-3 dolgu, bayt 4-7 büyük-uçlu uzunluk.
const (
	logHeaderLen = 8
	// streamStderr, başlığın ilk baytındaki stderr işareti.
	streamStderr = 2
	// logChunkSize, sink'e verilen en büyük parça. Bir kare bundan büyük
	// olabilir; o zaman parçalanarak akıtılır.
	//
	// Uzunluk alanı TELDEN geliyor ve 4 bayt, yani 4 GiB'a kadar bir değer
	// bildirebilir. Kareyi tek seferde tamponlamak, bozuk ya da kötü
	// davranan bir daemon'ın AYRICALIKLI süreçte bellek tüketmesine izin
	// verirdi. Sabit tampon bu sınıfı tamamen kapatır.
	logChunkSize = 32 << 10
)

// ContainerLogs, TEK bir replikanın çıktısını akıtır.
//
// Konteyner kimliği yine dışarıdan alınmaz: seçici matching() üzerinden
// KENDİ listemize çözülür, yani etiket doğrulaması bu yolda da geçerli.
// Tam olarak bir eşleşme bulunmalıdır — sıfır ise konteyner yok, birden
// çok ise seçici bir replikayı tekilleştirmemiş demektir ve hangisinin
// akıtılacağını tahmin etmek yanlış konteynerin günlüğünü göstermek olur.
func (c *Client) ContainerLogs(
	ctx context.Context, sel Selector, tailLines uint32, follow bool, since time.Time, sink Sink,
) error {
	matches, err := c.matching(ctx, sel)
	if err != nil {
		return err
	}
	if len(matches) != 1 {
		return fmt.Errorf("docker: günlük için tam olarak bir konteyner gerekli, %d bulundu", len(matches))
	}

	q := url.Values{"stdout": {"true"}, "stderr": {"true"}}
	if follow {
		q.Set("follow", "true")
	}
	if tailLines > 0 {
		q.Set("tail", strconv.FormatUint(uint64(tailLines), 10))
	}
	if !since.IsZero() {
		q.Set("since", strconv.FormatInt(since.Unix(), 10))
	}

	resp, err := c.do(ctx, http.MethodGet, "/containers/"+matches[0].ID+"/logs", q, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return demux(resp.Body, sink)
}

// demux, Docker'ın çoğullanmış günlük akışını ayırır.
//
// Bu çerçeveleme yalnızca konteynerin TTY'si KAPALIYKEN geçerlidir; TTY
// açıksa akış çıplak bayt olur ve başlık aranmaz. Panely'nin konteynerleri
// `Tty` alanını hiç göndermez, yani daima false — ölçüldü. createBody'ye
// bir gün Tty eklenirse BURASI da değişmek zorunda; bu yüzden alanın
// yokluğu container.go'da kasıtlı olarak belgeli.
func demux(r io.Reader, sink Sink) error {
	var hdr [logHeaderLen]byte
	buf := make([]byte, logChunkSize)

	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			// Temiz bitiş: akış kare sınırında kapandı.
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("docker: günlük başlığı okunamadı: %w", err)
		}

		isStderr := hdr[0] == streamStderr
		remaining := int64(binary.BigEndian.Uint32(hdr[4:logHeaderLen]))

		for remaining > 0 {
			n := int64(len(buf))
			if remaining < n {
				n = remaining
			}
			if _, err := io.ReadFull(r, buf[:n]); err != nil {
				// Kare yarıda kesildi. EOF burada temiz DEĞİL: başlık bir
				// uzunluk bildirdi ve o kadar bayt gelmedi.
				return fmt.Errorf("docker: günlük karesi eksik: %w", err)
			}
			if err := sink(buf[:n], isStderr); err != nil {
				return err
			}
			remaining -= n
		}
	}
}
