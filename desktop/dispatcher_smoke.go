//go:build windows && smoke

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/erkanrzgc/kadran/internal/sidecar"
)

// Duman testi ikilisi (`-tags smoke`): gerçek dağıtıcının yerine
// desktop/test/fixtures/fake-sidecar.mjs konuşur; fake@test hedefinde onay
// kutusu atlanır (bridge.Deps.Fixture). Bu dosya dağıtılan ikiliye GİRMEZ.
const smokeBuild = true

// fakeAnswer, sahte sunucunun yanıtı için üst sınır: takılan bir çağrı testi
// asmasın, sebebini söyleyerek düşsün.
const fakeAnswer = 60 * time.Second

// smokeUserData, testin kullanıcı verisi dizini: kullanıcının gerçek
// ayarlarına dokunulmaz.
func smokeUserData() string { return os.Getenv("KADRAN_SMOKE_USERDATA") }

type closer interface {
	Dispatch(ctx context.Context, method string, params json.RawMessage) (any, *sidecar.Error)
	Close()
}

type fakeReply struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *sidecar.Error  `json:"error"`
}

// fakeSidecar, sahte sunucuyla satır-bazlı JSON-RPC (kadran sidecar'ın biçimi).
type fakeSidecar struct {
	cmd *exec.Cmd
	in  io.WriteCloser

	mu      sync.Mutex
	next    int64
	pending map[int64]chan fakeReply
}

func newDispatcher(warn func(string)) closer {
	script := os.Getenv("KADRAN_SMOKE_SIDECAR")
	f := &fakeSidecar{pending: map[int64]chan fakeReply{}}
	if script == "" {
		warn("KADRAN_SMOKE_SIDECAR is not set")
		return f
	}
	// Yalnız depodaki sahte sunucu: mutlak yol, bilinen ad, var olan dosya.
	//nolint:gosec // yalnız duman ikilisi; koşucunun verdiği yol denetleniyor
	if info, err := os.Stat(script); err != nil || info.IsDir() || !filepath.IsAbs(script) || filepath.Base(script) != "fake-sidecar.mjs" {
		warn("KADRAN_SMOKE_SIDECAR must be the absolute path of test/fixtures/fake-sidecar.mjs")
		return f
	}
	cmd := exec.CommandContext(context.Background(), "node", script) //nolint:gosec // yalnız duman ikilisi; yol yukarıda doğrulandı
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		warn(err.Error())
		return f
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		warn(err.Error())
		return f
	}
	if err := cmd.Start(); err != nil {
		warn(fmt.Sprintf("could not start the fake sidecar: %v", err))
		return f
	}
	f.cmd, f.in = cmd, in
	go f.read(out, warn)
	return f
}

func (f *fakeSidecar) read(out io.Reader, warn func(string)) {
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for scanner.Scan() {
		var r fakeReply
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			warn(fmt.Sprintf("fake sidecar sent garbage: %v", err))
			continue
		}
		f.mu.Lock()
		ch := f.pending[r.ID]
		delete(f.pending, r.ID)
		f.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
}

func (f *fakeSidecar) Dispatch(_ context.Context, method string, params json.RawMessage) (any, *sidecar.Error) {
	fail := func(data string) *sidecar.Error {
		return &sidecar.Error{Code: sidecar.CodeInternalError, Message: "call failed", Data: data}
	}
	if f.in == nil {
		return nil, fail("the fake sidecar is not running")
	}
	ch := make(chan fakeReply, 1)
	f.mu.Lock()
	f.next++
	id := f.next
	f.pending[id] = ch
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err == nil {
		_, err = f.in.Write(append(line, '\n'))
	}
	f.mu.Unlock()
	if err != nil {
		return nil, fail(err.Error())
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return nil, r.Error
		}
		return r.Result, nil
	case <-time.After(fakeAnswer):
		return nil, fail("the fake sidecar did not answer " + method)
	}
}

func (f *fakeSidecar) Close() {
	if f.in != nil {
		_ = f.in.Close()
		_ = f.cmd.Wait()
	}
}
