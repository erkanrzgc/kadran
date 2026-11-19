package bridge

import (
	"errors"

	"github.com/erkanrzgc/kadran/desktop/internal/console"
	"github.com/erkanrzgc/kadran/desktop/internal/guard"
)

// Konsol yöntemleri. Mantık console.Runner'da: sınıflandırma, onay (yalnız
// Host.Confirm, Fixture atlatmaz), yönetici kapısı ve geçmiş orada. Buradaki
// tek iş girdiyi doğrulamak ve Runner'a vermek; arayüze hiçbir karar
// güvenilmez (K-165).

var errNoConsole = errors.New("the console is not available")

func (b *Bridge) runner() (*console.Runner, error) {
	if b.deps.Console == nil {
		return nil, errNoConsole
	}
	return b.deps.Console, nil
}

// ConsoleRun, satırı seçili sunucuda çalıştırır ve çalıştırma kimliğini
// döner. Çıktı ve sonuç Wails olaylarıyla gelir (console:*).
func (b *Bridge) ConsoleRun(server, line string) (string, error) {
	r, err := b.runner()
	if err != nil {
		return "", err
	}
	s, err := guard.ConsoleServer(server)
	if err != nil {
		return "", err
	}
	l, err := guard.ConsoleLine(line)
	if err != nil {
		return "", err
	}
	return r.Run(s, l)
}

// ConsoleInput, çalışan komutun bekleyen istemine bir satır verir.
func (b *Bridge) ConsoleInput(runID, text string) error {
	r, err := b.runner()
	if err != nil {
		return err
	}
	id, err := guard.RunID(runID)
	if err != nil {
		return err
	}
	t, err := guard.ConsoleInput(text)
	if err != nil {
		return err
	}
	return r.Input(id, t)
}

// ConsoleCancel, çalışan komutu iptal eder (Ctrl+C).
func (b *Bridge) ConsoleCancel(runID string) error {
	r, err := b.runner()
	if err != nil {
		return err
	}
	id, err := guard.RunID(runID)
	if err != nil {
		return err
	}
	return r.Cancel(id)
}

// ConsoleComplete, Tab tamamlama adaylarını döner; imleç UTF-16 birimidir.
func (b *Bridge) ConsoleComplete(server, line string, cursor int) ([]string, error) {
	r, err := b.runner()
	if err != nil {
		return nil, err
	}
	s, err := guard.ConsoleServer(server)
	if err != nil {
		return nil, err
	}
	l, err := guard.ConsoleLine(line)
	if err != nil {
		return nil, err
	}
	c, err := guard.ConsoleCursor(l, cursor)
	if err != nil {
		return nil, err
	}
	return r.Complete(s, l, c), nil
}

// ConsoleState, sunucunun geçmişi ve yönetici ayarı (panel açılışında).
func (b *Bridge) ConsoleState(server string) (console.State, error) {
	r, err := b.runner()
	if err != nil {
		return console.State{}, err
	}
	s, err := guard.ConsoleServer(server)
	if err != nil {
		return console.State{}, err
	}
	return r.State(s), nil
}

// ConsoleSetAdmin, "konsolda yönetici komutları" ayarını değiştirir. Açmak
// yerel onay kutusundan geçer, kapatmak geçmez. Ayar görünüm dosyasında değil,
// Runner'ın ayrı dosyasında; SaveAppearance ona dokunamaz.
func (b *Bridge) ConsoleSetAdmin(on bool) (bool, error) {
	r, err := b.runner()
	if err != nil {
		return false, err
	}
	return r.SetAdmin(on)
}
