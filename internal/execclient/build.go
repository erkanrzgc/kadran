package execclient

import (
	"context"
	"errors"
	"fmt"
	"io"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// BuildSink, derleme çıktısının tüketicisidir.
//
// Hata dönerse akış durdurulur; çağıranın (dağıtım akışı) istemciye
// yazamaması, derlemeye devam etmek için bir sebep değildir.
type BuildSink func(data []byte, isStderr bool) error

// ImageBuild, executor'a derleme yaptırır ve çıktıyı sink'e akıtır.
//
// Başarıda imaj kimliğini döner. Kimlik boşsa hata döner — bkz. aşağıda.
//
// ── ⚠ BAĞLAM: bu fonksiyon zaman aşımı EKLEMEZ ─────────────────────
//
// Bu paketteki her diğer çağıran `context.WithTimeout(ctx, DefaultTimeout)`
// deseniyle çağrılıyor ve o desen burada UYGULANAMAZ. Aynı hata bir
// katman aşağıda ölçüldü: dockerdrv'nin http.Client'ında blanket bir
// `Timeout` vardı ve `http.Client.Timeout` GÖVDE okumasını da kapsadığı
// için 60 saniyeden uzun HİÇBİR derleme başarılı olamıyordu (K-044).
// Gerçek sunucuda A/B: hatalı sürüm 1m00.0007s'de öldü, düzeltilmiş sürüm
// 1m16.14s'de tamamlandı.
//
// gRPC'de aynı tuzak `grpc-timeout` başlığıyla kurulur: bağlama konan
// süre TÜM AKIŞI kapsar, ilk yanıtı değil. Derleme dakikalarca sürebilir;
// sınır çağıranın bağlamından gelir ve o bağlam istemcinin bağlantısına
// bağlıdır. İstemci giderse akış zaten iptal olur.
func (c *Client) ImageBuild(
	ctx context.Context,
	req *panelyv1.ImageBuildRequest,
	sink BuildSink,
) (string, error) {
	stream, err := c.rpc.ImageBuild(ctx, req)
	if err != nil {
		return "", fmt.Errorf("derleme başlatılamadı: %w", err)
	}

	var imageID string
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("derleme akışı koptu: %w", err)
		}

		// Kimlik karesi ile günlük karesi aynı mesaj tipinde geliyor;
		// ayırt edici olan hangi alanın dolu olduğu. İkisi de aynı anda
		// dolu gelebilseydi sıralama önemli olurdu — executor öyle
		// göndermiyor ama burada da varsayılmıyor: her alan bağımsız
		// değerlendiriliyor.
		if id := msg.GetImageId(); id != "" {
			imageID = id
		}
		if data := msg.GetData(); len(data) > 0 {
			if err := sink(data, msg.GetIsStderr()); err != nil {
				return "", err
			}
		}
	}

	// Hata YOKLUĞU başarı DEĞİLDİR (K-042).
	//
	// Docker'ın klasik derleyicisi, derleme ortasında ölen bir yapı için
	// de HTTP 200 döner. Executor bunu kendi tarafında yakalıyor, ama
	// buradaki kontrol o katmana güvenmenin yerine geçmiyor: kimlik
	// gelmediyse elimizde kanıt yok ve kanıtsız başarı iddia edilemez.
	// Kontrol düzlemi şeması da imaj kimliği olmayan bir BUILT satırını
	// zaten kabul etmiyor.
	if imageID == "" {
		return "", errors.New(
			"executor derleme imaj kimliği döndürmedi — başarı kanıtlanamadı")
	}
	return imageID, nil
}
