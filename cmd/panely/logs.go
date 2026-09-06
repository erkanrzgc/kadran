package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// defaultTailLines, `-tail` verilmediğinde gösterilecek geçmiş satır
// sayısıdır.
//
// Sıfır olsaydı komut boş bir ekranla açılır ve kullanıcı çalışmadığını
// sanırdı; çok büyük olsaydı teşhis edilmek istenen son olay ekranın
// üstünden akıp giderdi. 200 satır bir terminal ekranından uzun, bir
// günlük dosyasından kısa.
const defaultTailLines = 200

// runLogs, canlı sürümün çıktısını akıtır.
func (c *cli) runLogs(ctx context.Context, args []string) int {
	fs := c.newFlagSet("logs")
	follow := fs.Bool("f", false, "akışı açık tut (Ctrl-C ile çık)")
	tail := fs.Uint("tail", defaultTailLines, "geçmişten kaç satır")

	// ── ⚠ Varsayılan sınır YOK ve bu KASITLI ────────────────────────
	//
	// `-f` ile akış saatlerce açık kalabilir; sabit bir sınır onu tam da
	// işe yaradığı anda keserdi. Aynı tuzak bu projede bir kez ölçüldü
	// (K-044) ve `deploy` komutunda da aynı sebeple sınırsız bırakıldı.
	//
	// Sınırsız bırakmak sorumsuzluk değil: bağlantı kurma aşamasının
	// kendi sınırı var (ssh ConnectTimeout) ve komut SIGINT'e duyarlı.
	timeout := fs.Duration("timeout", 0, "toplam süre sınırı (0 = sınırsız)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.usageError("kullanım: panely logs [-f] [-tail n] <uygulama> " +
			"[hedef] — seçenekler uygulama adından ÖNCE gelir")
	}

	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	conn, _, err := c.connect(ctx, fs.Arg(1))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	stream, err := conn.RPC().StreamLogs(ctx, &panelyv1.StreamLogsRequest{
		AppId:     fs.Arg(0),
		TailLines: uint32(*tail), //nolint:gosec // bayrak uint, sunucu ayrıca sınırlıyor
		Follow:    *follow,
	})
	if err != nil {
		return c.fail(fmt.Errorf("günlük akışı başlatılamadı: %w", err))
	}
	return c.consumeLogs(ctx, stream)
}

// consumeLogs, akışı okuyup uygun akıma yazar.
//
// ── stdout ve stderr AYRI tutuluyor ─────────────────────────────────
//
// Konteynerin stderr'i bizim stderr'imize gidiyor. Böylece
// `panely logs blog > app.log` yalnızca uygulamanın stdout'unu dosyaya
// yazar, hata satırları terminalde kalır — Docker'ın ayırdığı bilgiyi
// birleştirip atmak, akışları ayırmanın bütün faydasını yok ederdi.
func (c *cli) consumeLogs(
	ctx context.Context,
	stream panelyv1.PanelyService_StreamLogsClient,
) int {
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return exitOK
		}
		if err != nil {
			// ⚠ Ctrl-C hata DEĞİLDİR, zaman aşımı hatadır.
			//
			// `-f` akışını bitiren normal yol Ctrl-C'dir; kırmızı bir
			// mesajla bitirmek her normal çıkışta hata yazdırırdı.
			//
			// `-timeout` ile kesilme ise ayrı: kullanıcı çıkmadı, süre
			// doldu. İkisini birlikte yutmak, yarıda kalmış bir akışı
			// başarılı gösterirdi.
			if errors.Is(ctx.Err(), context.Canceled) {
				return exitOK
			}
			return c.fail(fmt.Errorf("günlük akışı koptu: %w", err))
		}

		out := c.stdout
		if msg.GetIsStderr() {
			out = c.stderr
		}
		if _, err := out.Write(msg.GetData()); err != nil {
			return c.fail(err)
		}
	}
}
