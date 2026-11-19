package console

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/internal/cli"
	"github.com/erkanrzgc/kadran/internal/servers"
)

// Olay adları. Köprü bunları olduğu gibi Wails olayı olarak yayar.
const (
	EventOutput = "console:output"
	EventInput  = "console:input"
	EventSecret = "console:secret"
	EventDone   = "console:done"
	EventUse    = "console:use"
	EventClear  = "console:clear"
)

// Çıktı akışları.
const (
	StreamOut = "out"
	StreamErr = "err"
)

// Event, arayüze giden bir konsol olayı. Data aşağıdaki türlerden biridir.
type Event struct {
	Name string
	Data any
}

// OutputData, console:output: çıktının bir parçası (en çok maxEventBytes).
type OutputData struct {
	Run    string `json:"run"`
	Stream string `json:"stream"`
	Data   string `json:"data"`
}

// InputData, console:input: komut stdin okumak istiyor. Prompt, satır sonu
// gelmemiş son çıktı satırıdır (CLI istemi ya da yazılı onay istemi).
type InputData struct {
	Run    string `json:"run"`
	Prompt string `json:"prompt"`
}

// SecretData, console:secret: gizli çıktı kartına giden metin (Tasarım 6a).
type SecretData struct {
	Run  string `json:"run"`
	Data string `json:"data"`
}

// DoneData, console:done: çalıştırmanın çıkış kodu.
type DoneData struct {
	Run  string `json:"run"`
	Exit int    `json:"exit"`
}

// UseData, console:use: seçili sunucu değişti.
type UseData struct {
	Server string `json:"server"`
}

// ClearData, console:clear: panel çıktısı temizlensin.
type ClearData struct{}

// State, panelin açılışta okuduğu durum.
type State struct {
	History []string `json:"history"`
	Admin   bool     `json:"admin"`
}

// Çalıştırıcı hataları. Ret nedenleri (ErrAdminOff gibi) dönülmez, çalıştırmanın
// stderr akışına yazılır ve done sıfır dışı kodla gelir; Run yalnız ErrBusy ve
// ErrClosed döner.
var (
	ErrBusy       = errors.New("a command is already running: wait for it or stop it with Ctrl+C")
	ErrClosed     = errors.New("the console is shutting down")
	ErrAdminOff   = errors.New("admin commands are off in the console: turn them on in Settings")
	ErrNoRun      = errors.New("no such running command")
	ErrNotWaiting = errors.New("the command is not waiting for input")
	ErrInputLine  = errors.New("input must be a single line")
)

// RunnerDeps, çalıştırıcının dış bağımlılıkları. Run nil ise cli.Run; Now nil
// ise time.Now; Language nil ise İngilizce; Conns nil ise CLI'ın kendi
// bağlantısı kullanılır.
type RunnerDeps struct {
	Run     func(context.Context, []string, cli.Env) int
	Confirm func(confirm.Box) (bool, error)
	// Emit, çıktı birleştirici kilidini TUTARKEN çağrılır: engellenmemeli ve
	// Runner'a geri çağrı yapmamalı.
	Emit     func(Event)
	Conns    *Conns
	History  History
	Admin    AdminSetting
	Log      AdminLog
	Servers  func() ([]servers.Server, error)
	Language func() confirm.Language
	Apps     func(ctx context.Context, server string) ([]string, error)
	Now      func() time.Time
}

// shutdownWait, Shutdown'ın iptal edilen komutun bitmesini en çok bekleme
// süresi (done ve yönetici kaydı yazılabilsin; asılı bir komut kapanışı
// sonsuza dek tutmasın).
const shutdownWait = 5 * time.Second

// Runner, konsol satırlarını Go tarafında sınıflandırır, onaylatır ve
// cli.Run'ı ayrı bir goroutine'de çalıştırır. Arayüz (K-165) hiçbir sınıf ya
// da onay kararına katılmaz: o yalnız satırı ve istem girdisini verir.
type Runner struct {
	d     RunnerDeps
	after afterFunc // birleştiricinin zamanlayıcısı; testler değiştirebilir

	mu     sync.Mutex
	active *runState
	closed bool

	confirmMu sync.Mutex // aynı anda tek onay kutusu
	apps      appsCache
}

// runState, süren bir çalıştırma.
type runState struct {
	id       string
	ctx      context.Context
	cancel   context.CancelFunc
	out      *batcher
	in       *promptReader
	finished chan struct{}
}

// NewRunner, bağımlılıklardan bir çalıştırıcı kurar.
func NewRunner(d RunnerDeps) *Runner {
	if d.Run == nil {
		d.Run = cli.Run
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Emit == nil {
		d.Emit = func(Event) {}
	}
	return &Runner{d: d, after: realAfter, apps: appsCache{entries: map[string]appsEntry{}}}
}

// Run, satırı seçili sunucuda çalıştırmak üzere bir çalıştırma başlatır ve
// kimliğini döner. Ayrıştırma, sınıf kapısı, onaylar ve komut ayrı bir
// goroutine'de yürür; her çalıştırma tam bir console:done ile biter.
func (r *Runner) Run(server, line string) (string, error) {
	id, err := newRunID()
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", ErrClosed
	}
	if r.active != nil {
		return "", ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	out := newBatcher(id, r.d.Emit, r.after)
	rs := &runState{id: id, ctx: ctx, cancel: cancel, out: out, in: newPromptReader(ctx, out), finished: make(chan struct{})}
	r.active = rs
	go r.execute(rs, server, line)
	return id, nil
}

// execute, çalıştırmayı yürütür ve bitirir. Yuva done'dan ÖNCE boşalır: done'u
// görür görmez yeni satır gönderen arayüz ErrBusy almamalı.
func (r *Runner) execute(rs *runState, server, line string) {
	exit := r.guarded(rs, server, line)
	rs.cancel()
	r.mu.Lock()
	if r.active == rs {
		r.active = nil
	}
	r.mu.Unlock()
	rs.out.finish(DoneData{Run: rs.id, Exit: exit})
	close(rs.finished)
}

// guarded, pipeline'ı çalıştırır. Bir komuttaki panik bütün masaüstünü
// düşürmesin diye yakalanır, stderr'e yazılır ve done hata koduyla gelir.
func (r *Runner) guarded(rs *runState, server, line string) (exit int) {
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(rs.out.writer(StreamErr), "console: internal error: %v\n", p)
			exit = cli.ExitError
		}
	}()
	return r.pipeline(rs, server, line)
}

// Input, çalışan komutun bekleyen istemine bir satır verir. Metin yalnız
// komutun stdin'ine ya da yazılı onay karşılaştırmasına gider; geçmişe, kayda
// ya da olaylara girmez.
func (r *Runner) Input(runID, text string) error {
	if strings.ContainsAny(text, "\r\n") {
		return ErrInputLine
	}
	rs := r.current(runID)
	if rs == nil {
		return ErrNoRun
	}
	return rs.in.deliver(text)
}

// Cancel, çalışmanın bağlamını iptal eder (Ctrl+C).
func (r *Runner) Cancel(runID string) error {
	rs := r.current(runID)
	if rs == nil {
		return ErrNoRun
	}
	rs.cancel()
	return nil
}

func (r *Runner) current(runID string) *runState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || r.active.id != runID {
		return nil
	}
	return r.active
}

// State, sunucunun geçmişini ve yönetici ayarını döner.
func (r *Runner) State(server string) State {
	history := r.d.History.Load(server)
	if history == nil {
		history = []string{}
	}
	return State{History: history, Admin: r.d.Admin.Load()}
}

// SetAdmin, "konsolda yönetici komutları" ayarını değiştirir. Açmak yerel
// onay kutusu ister ve yalnız (true, nil) cevabında kaydeder; kapatmak
// onaysızdır. Dönen değer ayarın son durumudur.
func (r *Runner) SetAdmin(on bool) (bool, error) {
	if !on {
		if err := r.d.Admin.Save(false); err != nil {
			return r.d.Admin.Load(), fmt.Errorf("console: save admin setting: %w", err)
		}
		return false, nil
	}
	lang := r.language()
	effect, ok := confirm.ConsoleEffect(lang, effectAdminKey)
	if !ok {
		return r.d.Admin.Load(), errors.New("console: no confirmation text for the admin setting")
	}
	approved, err := r.confirm(confirm.BuildConsole(lang, confirm.ConsoleRequest{Effect: effect}))
	if err != nil {
		return r.d.Admin.Load(), fmt.Errorf("console: confirm admin setting: %w", err)
	}
	if !approved {
		return r.d.Admin.Load(), nil
	}
	if err := r.d.Admin.Save(true); err != nil {
		return r.d.Admin.Load(), fmt.Errorf("console: save admin setting: %w", err)
	}
	return true, nil
}

// Shutdown, süren komutu iptal eder, bitmesini sınırlı bekler ve konsolun
// bağlantılarını kapatır. Sonraki Run'lar ErrClosed alır; ikinci çağrı
// etkisizdir.
func (r *Runner) Shutdown() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	rs := r.active
	r.mu.Unlock()
	if rs != nil {
		rs.cancel()
		select {
		case <-rs.finished:
		case <-time.After(shutdownWait):
		}
	}
	if r.d.Conns != nil {
		r.d.Conns.Close()
	}
}

// confirm, yerel onay kutusunu gösterir; aynı anda tek kutu. Confirm yoksa
// ret (kapalı varsayılan).
func (r *Runner) confirm(box confirm.Box) (bool, error) {
	r.confirmMu.Lock()
	defer r.confirmMu.Unlock()
	if r.d.Confirm == nil {
		return false, errors.New("no confirmation dialog is available")
	}
	return r.d.Confirm(box)
}

func (r *Runner) language() confirm.Language {
	if r.d.Language == nil {
		return confirm.English
	}
	return r.d.Language()
}

// newRunID, 16 küçük onaltılık haneli çalıştırma kimliği (crypto/rand).
func newRunID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("console: run id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
