package console

import (
	"bytes"
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// ── Çıktı birleştirici, istem okuyucu, gizli yazıcı ──────────────────

const (
	// flushEvery, küçük yazmaların birleştirildiği süre (spec Tasarım 3).
	flushEvery = 50 * time.Millisecond
	// maxEventBytes, bir console:output olayının en çok bayt sayısı. 10 MB'lık
	// tek bir satır ya da saniyede binlerce satır arayüzü dondurmasın.
	maxEventBytes = 32 << 10
	// maxPromptBytes, istem olarak gönderilen son satırın en çok uzunluğu.
	maxPromptBytes = 512
)

// afterFunc, d sonra fn'i çalıştıran zamanlayıcı; dönen işlev durdurur.
type afterFunc func(d time.Duration, fn func()) (stop func() bool)

func realAfter(d time.Duration, fn func()) func() bool { return time.AfterFunc(d, fn).Stop }

// segment, tek akıştan art arda gelen baytlar.
type segment struct {
	stream string
	data   []byte
}

// batcher, bir çalıştırmanın stdout ve stderr'ini TEK sıralı kuyrukta toplar
// (CLI istemleri ve yardım stderr'de, konsol satırları stdout'ta: sıra
// korunmalı). Küçük yazmalar en çok flushEvery bekler; maxEventBytes dolan
// kısım hemen gider. Çalıştırmanın BÜTÜN olayları (istem, gizli, use, clear,
// done) bekleyen çıktıyı önce boşaltıp aynı kilit altında yayılır: sıra bozulmaz.
type batcher struct {
	run   string
	emit  func(Event)
	after afterFunc

	mu       sync.Mutex
	segs     []segment
	size     int
	armed    bool
	stop     func() bool
	finished bool
	tail     []byte // son satır sonundan sonraki çıktı: istem metni
}

func newBatcher(run string, emit func(Event), after afterFunc) *batcher {
	return &batcher{run: run, emit: emit, after: after}
}

// streamWriter, birleştiricinin bir akışa bakan yazıcısı.
type streamWriter struct {
	b      *batcher
	stream string
}

func (w streamWriter) Write(p []byte) (int, error) {
	w.b.write(w.stream, p)
	return len(p), nil
}

func (b *batcher) writer(stream string) io.Writer { return streamWriter{b: b, stream: stream} }

func (b *batcher) write(stream string, p []byte) {
	if len(p) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trackTail(p)
	b.push(stream, p)
	for b.size >= maxEventBytes {
		b.emitFront()
	}
	if b.finished {
		b.flushLocked()
		return
	}
	if b.size > 0 && !b.armed {
		b.armed = true
		b.stop = b.after(flushEvery, b.onTimer)
	}
}

// push, baytları kopyalayarak kuyruğa ekler; aynı akışın son parçasına ekler.
func (b *batcher) push(stream string, p []byte) {
	if n := len(b.segs); n > 0 && b.segs[n-1].stream == stream {
		b.segs[n-1].data = append(b.segs[n-1].data, p...)
	} else {
		b.segs = append(b.segs, segment{stream: stream, data: append([]byte(nil), p...)})
	}
	b.size += len(p)
}

func (b *batcher) onTimer() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.armed = false
	b.flushLocked()
}

// emitFront, kuyruğun başından en çok maxEventBytes'lık bir olay yayar.
// Kesme rune sınırında yapılır: Wails veriyi JSON'a çevirir, yarım rune
// U+FFFD olur ve bayt sayısı bozulurdu.
func (b *batcher) emitFront() {
	seg := &b.segs[0]
	n := len(seg.data)
	if n > maxEventBytes {
		n = runeCut(seg.data, maxEventBytes)
	}
	b.emit(Event{Name: EventOutput, Data: OutputData{Run: b.run, Stream: seg.stream, Data: string(seg.data[:n])}})
	seg.data = seg.data[n:]
	b.size -= n
	switch {
	case len(seg.data) == 0:
		b.segs = b.segs[1:]
	case cap(seg.data) > 4*maxEventBytes && len(seg.data) < maxEventBytes:
		// Büyük bir yazmanın kalanı, dev diziyi tutmasın.
		seg.data = append([]byte(nil), seg.data...)
	}
}

// runeCut, data[:n]'nin bir rune'u bölmediği en büyük n ≤ max'ı döner;
// geçersiz UTF-8'de max.
func runeCut(data []byte, max int) int {
	for n := max; n > max-utf8.UTFMax && n > 0; n-- {
		if utf8.RuneStart(data[n]) {
			return n
		}
	}
	return max
}

func (b *batcher) flushLocked() {
	for len(b.segs) > 0 {
		b.emitFront()
	}
}

// trackTail, satır sonu gelmemiş son çıktıyı tutar (istem metni).
func (b *batcher) trackTail(p []byte) {
	if i := bytes.LastIndexByte(p, '\n'); i >= 0 {
		b.tail = append(b.tail[:0], p[i+1:]...)
	} else {
		b.tail = append(b.tail, p...)
	}
	if len(b.tail) > maxPromptBytes {
		cut := len(b.tail) - maxPromptBytes
		for cut < len(b.tail) && !utf8.RuneStart(b.tail[cut]) {
			cut++
		}
		b.tail = append([]byte(nil), b.tail[cut:]...)
	}
}

// event, bekleyen çıktıyı boşaltıp olayı yayar.
func (b *batcher) event(name string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushLocked()
	b.emit(Event{Name: name, Data: data})
}

// inputEvent, bekleyen çıktıyı boşaltıp istemi (son yarım satır) yayar.
func (b *batcher) inputEvent() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushLocked()
	b.emit(Event{Name: EventInput, Data: InputData{Run: b.run, Prompt: string(b.tail)}})
}

// resetTail: girdi teslim edildi; kullanıcının Enter'ı satırı bitirmiş sayılır.
func (b *batcher) resetTail() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tail = nil
}

// finish, zamanlayıcıyı durdurur, kalanı boşaltır ve done'u yayar. Geç gelen
// bir zamanlayıcı boş kuyruk bulur: done'dan sonra çıktı olayı gelmez.
func (b *batcher) finish(done DoneData) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.armed && b.stop != nil {
		b.stop()
	}
	b.armed = false
	b.flushLocked()
	b.finished = true
	b.emit(Event{Name: EventDone, Data: done})
}

// promptReader, komutun stdin'i. Read, bekleyen girdi yoksa console:input
// yayar ve Input'u (ya da iptali) bekler; girdiyi text+"\n" olarak verir.
// Kısa tamponla okunan kalan, yeni istem açmadan sonraki Read'lere gider.
type promptReader struct {
	ctx context.Context
	out *batcher

	mu      sync.Mutex
	waiting bool
	input   chan string // 1 tamponlu: deliver beklemeden bırakır

	pending []byte // yalnız okuyan goroutine dokunur
}

func newPromptReader(ctx context.Context, out *batcher) *promptReader {
	return &promptReader{ctx: ctx, out: out, input: make(chan string, 1)}
}

func (p *promptReader) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	if len(p.pending) == 0 {
		text, err := p.readText()
		if err != nil {
			return 0, err
		}
		p.pending = []byte(text + "\n")
	}
	n := copy(buf, p.pending)
	p.pending = p.pending[n:]
	return n, nil
}

// readText, istemi yayar ve bir girdi satırı bekler (satır sonu olmadan).
func (p *promptReader) readText() (string, error) {
	if err := p.ctx.Err(); err != nil {
		return "", err
	}
	p.mu.Lock()
	p.waiting = true
	p.mu.Unlock()
	p.out.inputEvent()
	select {
	case text := <-p.input:
		p.out.resetTail()
		return text, nil
	case <-p.ctx.Done():
		p.mu.Lock()
		p.waiting = false
		p.mu.Unlock()
		select { // iptalle yarışan bir teslimi bırak
		case <-p.input:
		default:
		}
		return "", p.ctx.Err()
	}
}

// deliver, bekleyen isteme girdiyi verir; istem yoksa ErrNotWaiting.
func (p *promptReader) deliver(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.waiting {
		return ErrNotWaiting
	}
	p.waiting = false
	p.input <- text
	return nil
}

// secretWriter, Env.Sensitive: her yazmayı yalnız console:secret olayına
// verir; çıktı kuyruğuna, istem metnine, geçmişe ve kayda girmez.
type secretWriter struct {
	out  *batcher
	used atomic.Bool
}

func (s *secretWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.used.Store(true)
	s.out.event(EventSecret, SecretData{Run: s.out.run, Data: string(p)})
	return len(p), nil
}

// hostKeyFailure, ssh'ın bilinmeyen/değişmiş host anahtarı hatası.
const hostKeyFailure = "Host key verification failed"

// hostKeyWatch, stderr'de hostKeyFailure'u yazma sınırlarını aşsa da arar.
type hostKeyWatch struct {
	mu    sync.Mutex
	carry []byte
	found bool
}

func (w *hostKeyWatch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.found {
		return len(p), nil
	}
	buf := append(append([]byte(nil), w.carry...), p...)
	if bytes.Contains(buf, []byte(hostKeyFailure)) {
		w.found, w.carry = true, nil
		return len(p), nil
	}
	if keep := len(hostKeyFailure) - 1; len(buf) > keep {
		buf = buf[len(buf)-keep:]
	}
	w.carry = append([]byte(nil), buf...)
	return len(p), nil
}

func (w *hostKeyWatch) seen() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.found
}
