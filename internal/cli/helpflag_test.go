package cli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// actionRecorder, bağlanma ve ssh başlatma girişimlerini sayar; ikisi de
// başarısız olur (hiçbir şey gerçekten çalışmasın).
type actionRecorder struct {
	mu       sync.Mutex
	dials    int
	commands int
}

func (a *actionRecorder) env(t *testing.T, stdout, stderr *bytes.Buffer) Env {
	home := t.TempDir()
	return Env{
		Stdin:  strings.NewReader(""),
		Stdout: stdout,
		Stderr: stderr,
		Dial: func(context.Context, string) (*client.Client, *kadranv1.PingResponse, error) {
			a.mu.Lock()
			a.dials++
			a.mu.Unlock()
			return nil, nil, errors.New("dial attempted")
		},
		Command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			a.mu.Lock()
			a.commands++
			a.mu.Unlock()
			return exec.CommandContext(ctx, "kadran-test-no-such-program-"+name, args...)
		},
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
	}
}

func (a *actionRecorder) counts() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dials, a.commands
}

// onaylı sınıftaki yapraklar, yol ile.
func confirmedLeaves(prefix []string, cmds []Command) [][]string {
	var out [][]string
	for _, c := range cmds {
		path := append(slices.Clone(prefix), c.Name)
		if len(c.Subs) > 0 {
			out = append(out, confirmedLeaves(path, c.Subs)...)
			continue
		}
		switch c.Class {
		case ClassChange, ClassDanger, ClassRoot:
			out = append(out, path)
		}
	}
	return out
}

// Konsol `X -h`'yi okuma sayıp onaysız çalıştırıyor (K-169, R7). Bu ancak
// onaylı sınıftaki hiçbir yaprak bayraklarını ayrıştırmadan önce bağlanmıyor
// ya da ssh başlatmıyorsa güvenli: her yaprak -h ile koşar, hiçbir girişim
// olmamalı ve yardım basılmış olmalı (deneme gerçekten -h yolundan geçti).
func TestHelpFlagNeverActs(t *testing.T) {
	leaves := confirmedLeaves(nil, Commands())
	if len(leaves) < 15 {
		t.Fatalf("yalnız %d onaylı yaprak: %v", len(leaves), leaves)
	}
	for _, path := range leaves {
		name := strings.Join(path, " ")
		t.Run(name, func(t *testing.T) {
			rec := &actionRecorder{}
			var stdout, stderr bytes.Buffer
			Run(context.Background(), append(slices.Clone(path), "-h"), rec.env(t, &stdout, &stderr))
			if dials, commands := rec.counts(); dials != 0 || commands != 0 {
				t.Fatalf("%s -h: %d bağlanma, %d ssh girişimi", name, dials, commands)
			}
			if !strings.Contains(stderr.String(), "Usage of "+name+":") {
				t.Fatalf("%s -h yardım basmadı:\nstdout %q\nstderr %q", name, &stdout, &stderr)
			}
		})
	}
}
