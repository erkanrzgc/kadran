package execclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// LogSink, günlük satırlarının tüketicisidir.
//
// Hata dönerse akış durdurulur: çağıranın istemciye yazamaması, executor'ı
// meşgul etmeye devam etmek için sebep değildir.
type LogSink func(data []byte, isStderr bool) error

// LogOptions, hangi replikanın hangi kısmının okunacağını söyler.
type LogOptions struct {
	AppID     string
	ReleaseID string
	Replica   uint32

	// TailLines, geçmişten kaç satır. 0 ise yalnızca yeni satırlar.
	TailLines uint32
	// Follow, akışı açık tutar.
	Follow bool
	// Since, bu andan öncesini eler. Sıfır ise sınır yok.
	Since time.Time
}

// ContainerLogs, bir replikanın çıktısını sink'e akıtır.
//
// ── ⚠ BAĞLAM: bu fonksiyon zaman aşımı EKLEMEZ ─────────────────────
//
// Bu paketteki diğer çağıranlar `context.WithTimeout(ctx, DefaultTimeout)`
// deseniyle sarılıyor; burada o desen YANLIŞ olurdu ve sebebi ImageBuild'
// dekiyle aynı: gRPC'de bağlama konan süre TÜM AKIŞI kapsar, ilk yanıtı
// değil. `follow` açıkken akış saatlerce sürebilir — bir zaman aşımı onu
// tam da işe yaradığı anda keserdi.
//
// Sınır çağıranın bağlamından gelir ve o bağlam istemcinin bağlantısına
// bağlı. İstemci giderse akış zaten iptal olur. Aynı tuzak bir katman
// aşağıda ölçülmüştü (K-044): http.Client.Timeout gövde okumasını da
// kapsadığı için 60 saniyeden uzun hiçbir derleme tamamlanamıyordu.
func (c *Client) ContainerLogs(
	ctx context.Context, opts LogOptions, sink LogSink,
) error {
	req := &panelyv1.ContainerLogsRequest{
		Ref: &panelyv1.ContainerRef{
			Release: &panelyv1.ReleaseRef{
				AppId:     opts.AppID,
				ReleaseId: opts.ReleaseID,
			},
			Replica: opts.Replica,
		},
		TailLines: opts.TailLines,
		Follow:    opts.Follow,
	}
	if !opts.Since.IsZero() {
		req.Since = timestamppb.New(opts.Since)
	}

	stream, err := c.rpc.ContainerLogs(ctx, req)
	if err != nil {
		return fmt.Errorf("günlük akışı başlatılamadı (%s/%s#%d): %w",
			opts.AppID, opts.ReleaseID, opts.Replica, err)
	}

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// ⚠ İPTAL hata değildir, ZAMAN AŞIMI hatadır.
			//
			// `follow` akışını bitiren normal yol, kullanıcının Ctrl-C'ye
			// basmasıdır; bunu hata diye raporlamak her normal çıkışta
			// kırmızı bir mesaj yazdırır ve gerçek kopmalar o gürültüde
			// kaybolurdu.
			//
			// Ama `context.DeadlineExceeded` AYRI bir şey: kullanıcı
			// çıkmadı, süre doldu. İkisini birlikte yutmak, `-timeout`
			// ile kesilen bir akışı başarılı gösterirdi.
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return fmt.Errorf("günlük akışı koptu: %w", err)
		}
		if data := msg.GetData(); len(data) > 0 {
			if err := sink(data, msg.GetIsStderr()); err != nil {
				return err
			}
		}
	}
}
