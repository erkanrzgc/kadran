package dockerdrv

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// ════════════════════════════════════════════════════════════════════
//  DERLEME AKIŞI
// ════════════════════════════════════════════════════════════════════
//
// Buradaki gövdeler UYDURULMADI: gerçek daemon'dan (Docker 29.1.3, git
// daemon ile kurulmuş üç depo) alınan çıktının aynısı. Uydurulmuş bir
// gövdeye karşı geçen test, yalnızca kendi hayal gücümüzü doğrular.

// collector, sink'e geleni toplar.
type collector struct {
	out, errOut strings.Builder
	fail        error
}

func (c *collector) sink(data []byte, isStderr bool) error {
	if c.fail != nil {
		return c.fail
	}
	if isStderr {
		c.errOut.Write(data)
	} else {
		c.out.Write(data)
	}
	return nil
}

const (
	// realSuccess, ÖLÇÜLMÜŞ başarılı derleme akışının kuyruğu.
	realSuccess = `{"stream":"Step 2/2 : RUN echo merhaba"}
{"stream":"\n"}
{"stream":"merhaba\n"}
{"aux":{"ID":"sha256:682293ee6cde4fa71a739b7349812ab946fbde3206380d37fc1ebc9c58cab570"}}
{"stream":"Successfully built 682293ee6cde\n"}
{"stream":"Successfully tagged panely/probe-ok:test\n"}
`
	// realMidBuildFailure, ÖLÇÜLMÜŞ derleme-ortası hatası.
	//
	// ⚠ Bu akış HTTP 200 ile geldi ve `aux` karesi HİÇ YOK.
	realMidBuildFailure = `{"stream":"Step 3/3 : RUN exit 3"}
{"stream":" ---> Running in c51325e89904\n"}
{"error":"The command '/bin/sh -c exit 3' returned a non-zero code: 3","errorDetail":{"code":3,"message":"The command '/bin/sh -c exit 3' returned a non-zero code: 3"}}
`
)

// TestBuildSuccessNeedsAuxFrame, başarının POZİTİF kanıta bağlı olduğunu
// doğrular.
func TestBuildSuccessNeedsAuxFrame(t *testing.T) {
	var c collector
	id, err := consumeBuild(strings.NewReader(realSuccess), c.sink)
	if err != nil {
		t.Fatalf("başarılı derleme hata döndürdü: %v", err)
	}
	if want := "sha256:682293ee6cde4fa71a739b7349812ab946fbde3206380d37fc1ebc9c58cab570"; id != want {
		t.Errorf("imaj kimliği %q, beklenen %q", id, want)
	}
	if !strings.Contains(c.out.String(), "merhaba\n") {
		t.Errorf("derleme çıktısı çağırana akmadı: %q", c.out.String())
	}
}

// TestBuildFailureIsCaughtDespiteHTTP200, bu dilimin ASIL tehlikesini
// sınar.
//
// Ölçüldü: derleme ortasında ölen bir derleme HTTP 200 döndürüyor. Durum
// koduna bakan bir uygulama bu akışı BAŞARILI sayardı ve bozuk bir imaj
// dağıtıma girerdi.
func TestBuildFailureIsCaughtDespiteHTTP200(t *testing.T) {
	var c collector
	id, err := consumeBuild(strings.NewReader(realMidBuildFailure), c.sink)
	if err == nil {
		t.Fatal("derleme-ortası hatası BAŞARILI sayıldı — HTTP 200 yeterli görülmüş")
	}
	if id != "" {
		t.Errorf("başarısız derleme imaj kimliği döndürdü: %q", id)
	}
	if !strings.Contains(err.Error(), "non-zero code: 3") {
		t.Errorf("hata sebebi taşınmadı: %v", err)
	}
	// Hata metni kullanıcıya stderr olarak gösterilmeli.
	if !strings.Contains(c.errOut.String(), "non-zero code: 3") {
		t.Errorf("hata çağırana stderr'den akmadı: %q", c.errOut.String())
	}
}

// TestBuildWithoutAnyEvidenceFails, ne hata ne aux karesi olan akışın
// başarısız sayıldığını doğrular.
//
// Bu, "hata karesi görmedim" ölçütünün YETMEDİĞİ durumdur: Docker
// ileride dördüncü bir başarısızlık biçimi eklerse ya da akış sessizce
// kesilirse, POZİTİF ölçüt olmadan başarı raporlanırdı.
func TestBuildWithoutAnyEvidenceFails(t *testing.T) {
	streams := map[string]string{
		"boş akış":         ``,
		"yalnızca çıktı":   `{"stream":"bir şeyler oldu\n"}` + "\n",
		"yarıda kesilmiş":  `{"stream":"Step 1/2"}` + "\n" + `{"stream":`,
		"tanınmayan biçim": `{"beklenmedik":"alan"}` + "\n",
	}
	for name, body := range streams {
		t.Run(name, func(t *testing.T) {
			var c collector
			if _, err := consumeBuild(strings.NewReader(body), c.sink); err == nil {
				t.Error("kanıtsız akış BAŞARILI sayıldı")
			}
		})
	}
}

// TestBuildForwardsPullProgress, taban imaj çekilirken gelen status
// karelerinin çağırana aktığını doğrular.
//
// Ölçüldü: alpine çekilirken 13 status karesi geldi ve hiçbiri `stream`
// taşımıyordu — ikisini tek çıkışta birleştirmenin dayanağı bu.
func TestBuildForwardsPullProgress(t *testing.T) {
	body := `{"status":"Pulling from library/alpine","id":"3.19"}
{"status":"Download complete"}
{"aux":{"ID":"sha256:abc"}}
`
	var c collector
	if _, err := consumeBuild(strings.NewReader(body), c.sink); err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	for _, want := range []string{"Pulling from library/alpine", "Download complete"} {
		if !strings.Contains(c.out.String(), want) {
			t.Errorf("çekme ilerlemesi akmadı: %q içinde %q yok", c.out.String(), want)
		}
	}
}

// TestBuildStopsWhenSinkFails, istemci koptuğunda akışın durduğunu
// doğrular. Denetim kaydının yazılabilmesi için sürücünün DÖNMESİ şart.
func TestBuildStopsWhenSinkFails(t *testing.T) {
	sentinel := errors.New("istemci koptu")
	c := collector{fail: sentinel}
	if _, err := consumeBuild(strings.NewReader(realSuccess), c.sink); !errors.Is(err, sentinel) {
		t.Errorf("sink hatası taşınmadı: %v", err)
	}
}

// TestBuildTagAndContextAreNotTakenFromCaller, etiketin KURULDUĞUNU ve
// çağıranın onu seçemediğini telde doğrular.
func TestBuildTagAndContextAreNotTakenFromCaller(t *testing.T) {
	f := newFakeDocker(t)
	c := f.client("/var/lib/panely/volumes")

	const sha = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"
	// Sürücü hata döndürecek (sahte daemon aux karesi üretmiyor); burada
	// ölçülen şey TELE NE GİTTİĞİ.
	_, _ = c.ImageBuild(context.Background(), BuildSpec{
		AppID: "blog", CommitSHA: sha,
		ContextURL: "https://github.com/o/r.git#" + sha,
		Dockerfile: "docker/prod.Dockerfile",
		BuildArgs:  map[string]string{"NODE_ENV": "production"},
	}, func([]byte, bool) error { return nil })

	op := f.op(t)
	q, err := url.ParseQuery(op.Query)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := q.Get("t"), "panely/blog:"+sha; got != want {
		t.Errorf("etiket %q, beklenen %q", got, want)
	}
	if got := q.Get("remote"); got != "https://github.com/o/r.git#"+sha {
		t.Errorf("bağlam URL'i %q", got)
	}
	if got := q.Get("dockerfile"); got != "docker/prod.Dockerfile" {
		t.Errorf("dockerfile %q", got)
	}
	if got := q.Get("buildargs"); got != `{"NODE_ENV":"production"}` {
		t.Errorf("buildargs %q", got)
	}
	// Ara konteynerler her durumda temizlenmeli.
	if q.Get("rm") != "true" || q.Get("forcerm") != "true" {
		t.Errorf("rm/forcerm ayarlanmadı: %q", op.Query)
	}
}

// TestStreamsOutliveTheRequestTimeout, akış uçlarının sabit bir zaman
// aşımına TAKILMADIĞINI, akış olmayanların ise TAKILDIĞINI doğrular.
//
// # Bu test neden var
//
// İstemcide `http.Client{Timeout: 60s}` vardı ve yanındaki yorum
// "yalnızca istek/yanıt turları içindir" diyordu. ÖLÇÜLDÜ ki yanlış:
// Timeout gövde okumasını da kapsıyor. Yani `logs -f` her dakika kopar,
// 60 sn'den uzun hiçbir derleme başarılı olamazdı.
//
// Yorumun andığı mekanizmanın gerçek olması gerekiyor; bu test onu
// gerçek tutuyor.
func TestStreamsOutliveTheRequestTimeout(t *testing.T) {
	// Sınırı kısalt ki test 60 sn beklemesin.
	old := requestTimeout
	requestTimeout = 150 * time.Millisecond
	t.Cleanup(func() { requestTimeout = old })

	// Sınırdan UZUN süren, kare kare akan bir günlük ucu.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(
				`{"Version":"29.1.3","ApiVersion":"1.48","MinAPIVersion":"1.44"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			_, _ = w.Write([]byte(`[{"Id":"aaa","State":"running","Created":0,"Labels":` +
				`{"panely.app_id":"blog","panely.release_id":"r1","panely.replica":"0"}}]`))
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Error("flush edilemiyor")
			return
		}
		for range 6 {
			_, _ = w.Write(frame(1, "tik\n"))
			fl.Flush()
			time.Sleep(60 * time.Millisecond)
		}
	}))
	t.Cleanup(srv.Close)

	c := &Client{http: srv.Client(), base: srv.URL, volumeRoot: "/var/lib/panely/volumes"}
	replica := uint32(0)
	var got collector
	err := c.ContainerLogs(context.Background(),
		Selector{AppID: "blog", ReleaseID: "r1", Replica: &replica},
		0, true, time.Time{}, got.sink)
	if err != nil {
		t.Fatalf("akış sınıra takıldı — logs -f her %v'de bir koparadı: %v", requestTimeout, err)
	}
	// 6 kare × 60 ms = ~360 ms; 150 ms'lik sınırın belirgin şekilde üstünde.
	if n := strings.Count(got.out.String(), "tik"); n != 6 {
		t.Errorf("%d kare alındı, 6 bekleniyordu — akış erken kesildi", n)
	}

	// ⚠ Yukarısı New()'i GÖRMÜYOR: httptest'in istemcisini kullanıyor.
	//
	// Mutasyon sınaması bu boşluğu gösterdi — `New()`'e blanket bir
	// Timeout geri konduğunda üstteki iddia yine geçti. Oysa hatanın
	// yaşadığı yer tam olarak New()'di. Ayırt edici kontrol doğrudan
	// oraya bakmalı.
	if to := New("/yok.sock", "/yok").http.Timeout; to != 0 {
		t.Errorf("New() istemciye %v'lik blanket Timeout koydu — "+
			"akış uçları (logs -f, uzun derlemeler) bu sınırda kopar", to)
	}
}

// TestNonStreamingCallsStayBounded, sınırın KALDIRILMADIĞINI doğrular.
//
// Üstteki testi geçirmenin ucuz yolu her sınırı silmekti; o zaman asılı
// kalan bir daemon ayrıcalıklı süreci sonsuza kadar bekletirdi.
func TestNonStreamingCallsStayBounded(t *testing.T) {
	old := requestTimeout
	requestTimeout = 100 * time.Millisecond
	t.Cleanup(func() { requestTimeout = old })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(
				`{"Version":"29.1.3","ApiVersion":"1.48","MinAPIVersion":"1.44"}`))
			return
		}
		time.Sleep(2 * time.Second) // asılı kalan daemon
	}))
	t.Cleanup(srv.Close)

	c := &Client{http: srv.Client(), base: srv.URL, volumeRoot: "/var/lib/panely/volumes"}
	start := time.Now()
	if _, err := c.ContainerList(context.Background(), "blog"); err == nil {
		t.Fatal("asılı kalan daemon çağrısı hiç zaman aşımına uğramadı")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("çağrı %v sürdü — sınır uygulanmıyor", elapsed)
	}
}

// ════════════════════════════════════════════════════════════════════
//  GÜNLÜK ÇERÇEVELEMESİ
// ════════════════════════════════════════════════════════════════════

// frame, sınama için tek bir günlük karesi kurar.
func frame(streamType byte, payload string) []byte {
	b := []byte{streamType, 0, 0, 0, 0, 0, 0, 0}
	n := len(payload)
	b[4] = byte(n >> 24)
	b[5] = byte(n >> 16)
	b[6] = byte(n >> 8)
	b[7] = byte(n)
	return append(b, payload...)
}

// TestDemuxMatchesMeasuredBytes, ÖLÇÜLEN baytları birebir çözer.
//
// Gerçek daemon'dan alınan ilk 24 bayt:
//
//	01 00 00 00 00 00 00 0d "cikti-satiri\n"
//	02 00 00 00 00 00 00 0c "hata-satiri\n"
func TestDemuxMatchesMeasuredBytes(t *testing.T) {
	raw := []byte{
		0x01, 0, 0, 0, 0, 0, 0, 0x0d,
		'c', 'i', 'k', 't', 'i', '-', 's', 'a', 't', 'i', 'r', 'i', '\n',
		0x02, 0, 0, 0, 0, 0, 0, 0x0c,
		'h', 'a', 't', 'a', '-', 's', 'a', 't', 'i', 'r', 'i', '\n',
	}
	var c collector
	if err := demux(bytes.NewReader(raw), c.sink); err != nil {
		t.Fatalf("çözümleme hatası: %v", err)
	}
	if got := c.out.String(); got != "cikti-satiri\n" {
		t.Errorf("stdout %q", got)
	}
	// stderr AYRI tutulmalı: istemci ikisini farklı gösterebilsin.
	if got := c.errOut.String(); got != "hata-satiri\n" {
		t.Errorf("stderr %q", got)
	}
}

// TestDemuxStreamsFramesLargerThanBuffer, tampondan büyük kareyi
// parçalayarak akıttığını doğrular.
//
// Uzunluk alanı TELDEN geliyor; kareyi tek seferde tamponlamak ayrıcalıklı
// süreçte sınırsız bellek ayırmak olurdu.
func TestDemuxStreamsFramesLargerThanBuffer(t *testing.T) {
	big := strings.Repeat("x", logChunkSize*2+7)
	var c collector
	if err := demux(bytes.NewReader(frame(1, big)), c.sink); err != nil {
		t.Fatalf("çözümleme hatası: %v", err)
	}
	if got := c.out.String(); got != big {
		t.Errorf("büyük kare bozuldu: %d bayt geldi, %d beklendi", len(got), len(big))
	}
}

// TestDemuxRejectsTruncatedFrame, başlığın bildirdiği kadar bayt
// gelmediğinde SESSİZCE bitmediğini doğrular.
func TestDemuxRejectsTruncatedFrame(t *testing.T) {
	full := frame(1, "tam-yuk")
	var c collector
	// Son iki baytı kes: başlık 7 bayt diyor, 5 bayt var.
	if err := demux(bytes.NewReader(full[:len(full)-2]), c.sink); err == nil {
		t.Error("eksik kare temiz bitiş sayıldı — kesilen günlük tam sanılırdı")
	}
}

// TestDemuxCleanEOFAtFrameBoundary, kare sınırında kapanan akışın hata
// üretmediğini doğrular.
func TestDemuxCleanEOFAtFrameBoundary(t *testing.T) {
	var c collector
	if err := demux(bytes.NewReader(frame(1, "bitti\n")), c.sink); err != nil {
		t.Errorf("temiz bitiş hata döndürdü: %v", err)
	}
}

// TestLogsNeedExactlyOneContainer, seçicinin tekilleştirilmiş olmasını
// zorunlu kıldığını doğrular.
//
// Sıfır eşleşmede "boş günlük" döndürmek, silinmiş bir konteynerin
// günlüğünü BOŞ ama BAŞARILI göstermek olurdu. Birden çok eşleşmede
// birini seçmek, yanlış replikanın günlüğünü göstermek olurdu.
func TestLogsNeedExactlyOneContainer(t *testing.T) {
	dup := listEntry{
		ID: "aaa", State: "running",
		Labels: map[string]string{
			labelAppID: "blog", labelReleaseID: "r1", labelReplica: "0",
		},
	}
	cases := map[string][]listEntry{
		"hiç yok":    {},
		"birden çok": {dup, {ID: "bbb", State: "running", Labels: dup.Labels}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeDocker(t)
			f.containers = entries
			replica := uint32(0)
			err := f.client("/var/lib/panely/volumes").ContainerLogs(
				context.Background(),
				Selector{AppID: "blog", ReleaseID: "r1", Replica: &replica},
				0, false, time.Time{}, func([]byte, bool) error { return nil })
			if err == nil {
				t.Fatal("tekil olmayan seçici kabul edildi")
			}

			// ⚠ "hata döndü" YETMEZ.
			//
			// Mutasyon sınaması bunu yakaladı: kontrol `!= 1` yerine `< 1`
			// yapıldığında iki eşleşmeli durum YİNE hata döndürüyordu —
			// ama çokluk kontrolünden değil, sahte daemon'ın günlük
			// biçiminde olmayan gövdesinden. Test doğru sonucu YANLIŞ
			// sebeple geçiyordu.
			//
			// Ayırt edici ölçüt davranışsal: istek TELE HİÇ ÇIKMAMALI.
			for _, r := range f.requests {
				if strings.HasSuffix(r.Path, "/logs") {
					t.Errorf("tekil olmayan seçiciyle günlük isteği gönderildi: %s", r.Path)
				}
			}
		})
	}
}

// TestLogsQueryCarriesBothStreams, stdout ve stderr'in İKİSİNİN de
// istendiğini doğrular. Biri unutulsaydı hata satırları kaybolurdu.
func TestLogsQueryCarriesBothStreams(t *testing.T) {
	f := newFakeDocker(t)
	f.containers = []listEntry{{
		ID: "aaa", State: "running",
		Labels: map[string]string{
			labelAppID: "blog", labelReleaseID: "r1", labelReplica: "0",
		},
	}}
	replica := uint32(0)
	_ = f.client("/var/lib/panely/volumes").ContainerLogs(
		context.Background(),
		Selector{AppID: "blog", ReleaseID: "r1", Replica: &replica},
		50, true, time.Unix(1750000000, 0),
		func([]byte, bool) error { return nil })

	var logReq recorded
	for _, r := range f.requests {
		if strings.HasSuffix(r.Path, "/logs") {
			logReq = r
		}
	}
	if logReq.Path == "" {
		t.Fatal("günlük isteği hiç gitmedi")
	}
	q, err := url.ParseQuery(logReq.Query)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"stdout", "stderr", "follow"} {
		if q.Get(k) != "true" {
			t.Errorf("%s istenmedi: %q", k, logReq.Query)
		}
	}
	if q.Get("tail") != "50" {
		t.Errorf("tail %q", q.Get("tail"))
	}
	if q.Get("since") != "1750000000" {
		t.Errorf("since %q", q.Get("since"))
	}
	// Konteyner kimliği BİZİM listemizden gelmeli.
	if !strings.Contains(logReq.Path, "aaa") {
		t.Errorf("yol beklenen konteynere gitmedi: %q", logReq.Path)
	}
}
