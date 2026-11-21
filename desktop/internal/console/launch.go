package console

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/erkanrzgc/kadran/internal/cli"
	"github.com/erkanrzgc/kadran/internal/client"
)

// strictHostKey, root yolundaki ssh'a eklenen seçenek: host anahtarı
// known_hosts'ta yoksa ssh bağlanmaz (kullanıcının ssh_config'i accept-new ya
// da no dese bile; komut satırındaki -o yapılandırmadan önce gelir).
const strictHostKey = "StrictHostKeyChecking=yes"

// secretNotice, gizli çıktı karta gittiğinde konsol satırına yazılan not.
const secretNotice = "secret output shown in a separate card\n"

// runIO, bir çalıştırmanın Env'i ve sonradan bakılan yan yazıcıları.
type runIO struct {
	env    cli.Env
	secret *secretWriter
	hostKV *hostKeyWatch // yalnız root
}

// newRunIO, komutun Env'ini kurar: stdout/stderr birleştiriciye, Sensitive
// yalnız gizli olaya, Stdin istem okuyucuya, Dial konsolun bağlantısına,
// Command pencere gizleyen kancaya (root'ta StrictHostKeyChecking ile).
func (r *Runner) newRunIO(rs *runState, root bool) runIO {
	rio := runIO{secret: &secretWriter{out: rs.out}}
	stderr := rs.out.writer(StreamErr)
	if root {
		rio.hostKV = &hostKeyWatch{}
		stderr = io.MultiWriter(stderr, rio.hostKV)
	}
	rio.env = cli.Env{
		Stdin:     rs.in,
		Stdout:    rs.out.writer(StreamOut),
		Stderr:    stderr,
		Sensitive: rio.secret,
		Command:   commandHook(root),
	}
	if r.d.Conns != nil {
		rio.env.Dial = r.d.Conns.Dial
	}
	return rio
}

// commandHook, CLI'ın ssh başlattığı her yerden geçen kanca. Konsol
// penceresi gizlenir (-H windowsgui ile derlenen masaüstü bir an pencere
// açmasın); root sınıfında ssh'ın argümanlarının BAŞINA
// `-o StrictHostKeyChecking=yes` eklenir (hedeften önce).
func commandHook(root bool) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if root {
			args = append([]string{"-o", strictHostKey}, args...)
		}
		cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // program adı CLI'dan (ssh), kabuksuz argv
		client.HideConsole(cmd)
		return cmd
	}
}

// launch, onaylanmış komutu cli.Run ile çalıştırır; ardından gizli çıktı
// notunu, bilinmeyen host ipucunu ve (root'ta) yönetici kaydını yazar.
//
// Yönetici kaydı defer'da: cli.Run paniklese de (ssh çalışmış olabilir) satır
// çıkış 1 ile düşer. Panik burada yakalanmaz; guarded yakalar, done ve çıkış
// kodu değişmez.
func (r *Runner) launch(rs *runState, p plan) int {
	start := r.d.Now()
	rio := r.newRunIO(rs, p.root())
	exit := cli.ExitError
	if p.root() {
		defer func() { r.logAdmin(rs, p, exit, start) }()
	}
	exit = r.d.Run(rs.ctx, p.full, rio.env)
	if rio.secret.used.Load() {
		fmt.Fprint(rio.env.Stdout, secretNotice)
	}
	if p.root() && exit != cli.ExitOK && rio.hostKV.seen() {
		fmt.Fprintf(rio.env.Stderr, "connect once from the CLI first (kadran key list -s %s)\n", quote(p.server.Name))
	}
	return exit
}

// logAdmin, root komutunun yerel kaydını yazar: UTC zaman, sunucu, arıtılmış
// komut, çıkış kodu, süre. İstem girdisi ve gizli çıktı buraya hiç gelmez.
func (r *Runner) logAdmin(rs *runState, p plan, exit int, start time.Time) {
	entry := AdminEntry{
		Time:     start.UTC(),
		Server:   p.server.Name,
		Command:  p.line,
		Exit:     exit,
		Duration: r.d.Now().Sub(start),
	}
	if err := r.d.Log.Append(entry); err != nil {
		fmt.Fprintf(rs.out.writer(StreamErr), "console admin log could not be written: %v\n", err)
	}
}
