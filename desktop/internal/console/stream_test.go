package console

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// fakeClock, birleştiricinin zamanlayıcısını testten tetikletir.
type fakeClock struct {
	mu      sync.Mutex
	pending []func()
	delays  []time.Duration
}

func (f *fakeClock) after(d time.Duration, fn func()) func() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delays = append(f.delays, d)
	stopped := false
	f.pending = append(f.pending, func() {
		if !stopped {
			fn()
		}
	})
	return func() bool {
		stopped = true
		return true
	}
}

// fire, bekleyen zamanlayıcıları çalıştırır; kaç tane olduğunu döner.
func (f *fakeClock) fire() int {
	f.mu.Lock()
	fns := f.pending
	f.pending = nil
	f.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
	return len(fns)
}

// collector, olayları sırayla toplar.
type collector struct {
	mu     sync.Mutex
	events []Event
}

func (c *collector) emit(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *collector) outputs() []OutputData {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []OutputData
	for _, e := range c.events {
		if d, ok := e.Data.(OutputData); ok && e.Name == EventOutput {
			out = append(out, d)
		}
	}
	return out
}

func (c *collector) names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, e := range c.events {
		out = append(out, e.Name)
	}
	return out
}

// Olay sınırı sözleşme: arayüz (K-165) olay başına en çok 32 KiB bekler; testler
// sabiti kendisine göre ölçtüğü için değeri burada ayrıca sabitlenir.
func TestEventLimitIsThirtyTwoKiB(t *testing.T) {
	if maxEventBytes != 32<<10 {
		t.Fatalf("maxEventBytes = %d, sözleşme %d", maxEventBytes, 32<<10)
	}
}

func TestBatchWriterSplitsLargeWrites(t *testing.T) {
	clock := &fakeClock{}
	col := &collector{}
	b := newBatcher("r1", col.emit, clock.after)

	// 10 MiB tek yazma; çok baytlı karakterler sınırın rune ortasına denk
	// gelebilsin diye karışık.
	big := bytes.Repeat([]byte("abcçğ\n"), (10<<20)/8+1)[:10<<20]
	if _, err := b.writer(StreamOut).Write(big); err != nil {
		t.Fatal(err)
	}
	// Dolan parçalar zamanlayıcıyı beklemeden gider: 10 MiB bellekte birikmez.
	if early := len(col.outputs()); early < (10<<20)/maxEventBytes-1 {
		t.Fatalf("zamanlayıcıdan önce yalnız %d olay", early)
	}
	b.finish(DoneData{Run: "r1"})

	var total bytes.Buffer
	outs := col.outputs()
	if len(outs) < (10<<20)/maxEventBytes {
		t.Fatalf("yalnız %d olay", len(outs))
	}
	for i, o := range outs {
		if len(o.Data) > maxEventBytes {
			t.Fatalf("olay %d: %d bayt > %d", i, len(o.Data), maxEventBytes)
		}
		if i < len(outs)-1 && !utf8.ValidString(o.Data) {
			t.Fatalf("olay %d rune ortasından kesildi", i)
		}
		if o.Run != "r1" || o.Stream != StreamOut {
			t.Fatalf("olay %d: %+v", i, o)
		}
		total.WriteString(o.Data)
	}
	if !bytes.Equal(total.Bytes(), big) {
		t.Fatalf("toplam bayt korunmadı: %d / %d", total.Len(), len(big))
	}
	if names := col.names(); names[len(names)-1] != EventDone {
		t.Fatalf("son olay done değil: %v", names[len(names)-1])
	}
}

func TestBatchWriterMergesSmallWrites(t *testing.T) {
	clock := &fakeClock{}
	col := &collector{}
	b := newBatcher("r1", col.emit, clock.after)
	out, errw := b.writer(StreamOut), b.writer(StreamErr)

	for _, s := range []string{"a", "b", "c"} {
		_, _ = out.Write([]byte(s))
	}
	if n := len(col.outputs()); n != 0 {
		t.Fatalf("zamanlayıcıdan önce %d olay", n)
	}
	if fired := clock.fire(); fired != 1 {
		t.Fatalf("%d zamanlayıcı kuruldu, 1 olmalı", fired)
	}
	if outs := col.outputs(); len(outs) != 1 || outs[0].Data != "abc" {
		t.Fatalf("50 ms içindeki yazmalar tek olay olmalı: %+v", outs)
	}
	for _, d := range clock.delays {
		if d != flushEvery || flushEvery != 50*time.Millisecond {
			t.Fatalf("birleştirme süresi %v", d)
		}
	}

	_, _ = out.Write([]byte("1"))
	_, _ = errw.Write([]byte("2"))
	_, _ = out.Write([]byte("3"))
	clock.fire()
	outs := col.outputs()[1:]
	want := []OutputData{{"r1", StreamOut, "1"}, {"r1", StreamErr, "2"}, {"r1", StreamOut, "3"}}
	if len(outs) != len(want) {
		t.Fatalf("akış sırası: %+v", outs)
	}
	for i := range want {
		if outs[i] != want[i] {
			t.Fatalf("akış sırası: %+v", outs)
		}
	}
}

func TestBatchWriterFlushesBeforeOtherEvents(t *testing.T) {
	clock := &fakeClock{}
	col := &collector{}
	b := newBatcher("r1", col.emit, clock.after)
	_, _ = b.writer(StreamOut).Write([]byte("line\n"))
	s := &secretWriter{out: b}
	_, _ = s.Write([]byte("SECRET"))
	_, _ = b.writer(StreamOut).Write([]byte("tail"))
	b.finish(DoneData{Run: "r1", Exit: 0})
	clock.fire()

	got := col.names()
	want := []string{EventOutput, EventSecret, EventOutput, EventDone}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("olay sırası %v, want %v", got, want)
	}
	for _, o := range col.outputs() {
		if strings.Contains(o.Data, "SECRET") {
			t.Fatal("gizli değer çıktı olayına girdi")
		}
	}
	if !s.used.Load() {
		t.Fatal("gizli yazıcı kullanıldığını bilmiyor")
	}
}

func TestPromptReaderWaitsForInput(t *testing.T) {
	clock := &fakeClock{}
	col := &collector{}
	b := newBatcher("r1", col.emit, clock.after)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := newPromptReader(ctx, b)
	_, _ = b.writer(StreamErr).Write([]byte("first\nrecovery code: "))

	if err := p.deliver("early"); !errors.Is(err, ErrNotWaiting) {
		t.Fatalf("istem yokken teslim: %v", err)
	}
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 2)
		var all strings.Builder
		for !strings.HasSuffix(all.String(), "\n") {
			n, err := p.Read(buf)
			all.Write(buf[:n])
			if err != nil {
				break
			}
		}
		got <- all.String()
	}()
	waitFor(t, func() bool { return countName(col, EventInput) == 1 })
	if err := p.deliver("ab-cd"); err != nil {
		t.Fatal(err)
	}
	if s := <-got; s != "ab-cd\n" {
		t.Fatalf("okunan %q", s)
	}
	if n := countName(col, EventInput); n != 1 {
		t.Fatalf("%d istem olayı", n)
	}
	if prompt := lastPrompt(col); prompt != "recovery code: " {
		t.Fatalf("istem %q", prompt)
	}
}

func TestPromptReaderCancelEndsRead(t *testing.T) {
	col := &collector{}
	b := newBatcher("r1", col.emit, (&fakeClock{}).after)
	ctx, cancel := context.WithCancel(context.Background())
	p := newPromptReader(ctx, b)
	errc := make(chan error, 1)
	go func() {
		_, err := p.Read(make([]byte, 8))
		errc <- err
	}()
	waitFor(t, func() bool { return countName(col, EventInput) == 1 })
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("iptal: %v", err)
	}
	if err := p.deliver("late"); !errors.Is(err, ErrNotWaiting) {
		t.Fatalf("iptalden sonra teslim: %v", err)
	}
	if _, err := p.Read(make([]byte, 8)); !errors.Is(err, context.Canceled) {
		t.Fatalf("iptalden sonra okuma: %v", err)
	}
}

// Girdi teslim edilince istem satırı biter: sonraki istem öncekini taşımaz.
func TestPromptResetsAfterInput(t *testing.T) {
	col := &collector{}
	b := newBatcher("r1", col.emit, (&fakeClock{}).after)
	p := newPromptReader(context.Background(), b)
	for i, label := range []string{"code: ", "again: "} {
		_, _ = b.writer(StreamErr).Write([]byte(label))
		done := make(chan string, 1)
		go func() {
			text, _ := p.readText()
			done <- text
		}()
		waitFor(t, func() bool { return countName(col, EventInput) == i+1 })
		if got := lastPrompt(col); got != label {
			t.Fatalf("istem %d: %q, want %q", i, got, label)
		}
		if err := p.deliver("x"); err != nil {
			t.Fatal(err)
		}
		<-done
	}
}

func lastPrompt(c *collector) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	prompt := ""
	for _, e := range c.events {
		if d, ok := e.Data.(InputData); ok {
			prompt = d.Prompt
		}
	}
	return prompt
}

func TestHostKeyWatchAcrossWrites(t *testing.T) {
	w := &hostKeyWatch{}
	for _, part := range []string{"Warning\nHost key ver", "ification fail", "ed.\n"} {
		_, _ = w.Write([]byte(part))
	}
	if !w.seen() {
		t.Fatal("parçalı yazılan hata görülmedi")
	}
	quiet := &hostKeyWatch{}
	_, _ = quiet.Write([]byte("Permission denied (publickey).\n"))
	if quiet.seen() {
		t.Fatal("başka hata host anahtarı sayıldı")
	}
}

func countName(c *collector, name string) int {
	n := 0
	for _, got := range c.names() {
		if got == name {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("koşul gerçekleşmedi")
		}
		time.Sleep(time.Millisecond)
	}
}
