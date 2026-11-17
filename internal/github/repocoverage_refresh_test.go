package github

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

const oneInstallation = `{"total_count":1,"installations":[{"id":7,"account":{"login":"octo"},"repository_selection":"selected"}]}`

type decideResult struct {
	d   Decision
	err error
}

func writeList(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(oneInstallation)) }

// Decide bir yenilemeyi beklerken yeni bağlantı o yenilemeyi iptal eder
// (invalidateCoverage(true): kuşak artar, sonuç atılır, yenisi başlar).
// Bekleyen, boş önbellekten verisiz NotCovered dönmemeli: yeni yenilemeye
// aynı ctx bütçesiyle katılmalı ve gerçek kararı vermeli.
func TestDecideRejoinsRefreshThatSupersededItsOwn(t *testing.T) {
	gh := connectedGH()
	first, second := make(chan struct{}, 1), newGate()
	var calls atomic.Int32
	gh.handle("GET", "/user/installations", func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1: // yerini yenisine bırakacak yenileme: iptal edilene dek takılı
			first <- struct{}{}
			<-r.Context().Done()
		case 2:
			second.wrap(writeList)(w, r)
		default:
			writeList(w, r)
		}
	})
	e := newEnv(t, gh, false, nil) // ısıtma yok: ilk yenilemeyi Decide başlatır
	t.Cleanup(second.open)
	if err := e.s.tk.install(sampleState()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res := make(chan decideResult, 1)
	go func() {
		d, err := e.s.Decide(ctx, "octo", "priv")
		res <- decideResult{d, err}
	}()
	select {
	case <-first:
	case <-time.After(10 * time.Second):
		t.Fatal("the first refresh never reached GitHub")
	}
	e.s.covMu.Lock()
	old := e.s.refresh
	e.s.covMu.Unlock()

	e.s.invalidateCoverage(true) // yeni bağlantı

	second.waitEntered(t)
	select {
	case <-old.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the superseded refresh did not finish")
	}
	select {
	case r := <-res:
		t.Fatalf("Decide = %v, %v while the newer refresh was still running", r.d, r.err)
	case <-time.After(300 * time.Millisecond):
	}
	second.open()
	select {
	case r := <-res:
		if r.d != DecisionArchive || r.err != nil {
			t.Fatalf("Decide = %v, %v; want DecisionArchive from the newer refresh", r.d, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Decide did not return")
	}
}

// Listede herkese açık görünen bir kayıt 5 dakikadan eskiyse karar BEKLEMEDEN
// döner ve tek bir arka plan yenilemesi başlar: depo özele çevrildiyse sonraki
// derleme arşiv yoluna geçer. Tekrarlanan isabetler ikinci yenileme başlatmaz.
func TestStalePublicHitRechecksInBackground(t *testing.T) {
	gh := connectedGH()
	g := newGate()
	var calls atomic.Int32
	gh.handle("GET", "/user/installations", func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 { // açılıştaki ısıtma
			writeList(w, r)
			return
		}
		g.wrap(writeList)(w, r)
	})
	e := newEnv(t, gh, true, nil)
	t.Cleanup(g.open)
	e.waitCoverage(t)
	ctx := context.Background()
	if d, err := e.s.Decide(ctx, "octo", "pub"); d != DecisionNotCovered || err != nil {
		t.Fatalf("fresh public hit = %v, %v", d, err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("a fresh public hit refreshed: %d list requests", n)
	}

	e.clock.Advance(publicRecheckAge + time.Second)
	for i := 0; i < 3; i++ {
		dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		d, err := e.s.Decide(dctx, "octo", "pub")
		cancel()
		if d != DecisionNotCovered || err != nil {
			t.Fatalf("stale public hit %d = %v, %v; want an immediate NotCovered", i, d, err)
		}
	}
	g.waitEntered(t) // yenileme GitHub'da beklerken üç karar da döndü
	g.open()
	e.waitCoverage(t)
	if n := calls.Load(); n != 2 {
		t.Fatalf("installation list requests = %d, want exactly one background recheck", n)
	}
}
