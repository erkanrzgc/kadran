// Package console, masaüstü konsolunun saf parçalarını içerir: satır
// ayrıştırma, sunucu bağlamı, gizli değer arıtma ve yerel dosyalar.
package console

import (
	"errors"
	"strings"

	"github.com/erkanrzgc/kadran/desktop/internal/guard"
)

// MaxLineBytes, bir konsol satırının en çok bayt uzunluğu.
const MaxLineBytes = 4096

// Satır ayrıştırma hataları. Metinler kullanıcıya konsolda gösterilir.
var (
	ErrTooLong = errors.New("the line is too long (4096 bytes at most)")
	ErrControl = errors.New("the line contains control or invisible characters")
	ErrShell   = errors.New("the console is not a shell: |, >, <, ;, &, ` and $( are not supported")
	ErrQuote   = errors.New("unterminated quote")
)

// ParseLine, konsol satırını sözcüklere böler. Kabuk değildir: tırnak dışında
// kabuk işleçleri reddedilir, tırnak içi olduğu gibi kalır; ters bölü yoktur.
// Satır sonu dahil her denetim/biçim karakteri (yapıştırılmış çok satır)
// reddedilir, böylece ikinci satır sessizce çalışmaz.
func ParseLine(line string) ([]string, error) {
	if len(line) > MaxLineBytes {
		return nil, ErrTooLong
	}
	if guard.HasControlOrFormat(line) {
		return nil, ErrControl
	}
	p := wordParser{}
	for i := 0; i < len(line); i++ {
		if err := p.step(line, i); err != nil {
			return nil, err
		}
	}
	if p.quote != 0 {
		return nil, ErrQuote
	}
	p.flush()
	return p.words, nil
}

// wordParser, ParseLine'ın durumu: açık tırnak ve biriken sözcük.
type wordParser struct {
	words  []string
	cur    strings.Builder
	inWord bool
	quote  byte // 0, '"' ya da '\''
}

func (p *wordParser) flush() {
	if p.inWord {
		p.words = append(p.words, p.cur.String())
		p.cur.Reset()
		p.inWord = false
	}
}

// step, line[i] baytını işler. Çok baytlı karakterlerin bayt parçaları ASCII
// işleçlerle çakışmaz (UTF-8), bu yüzden bayt bayt gezmek güvenlidir.
func (p *wordParser) step(line string, i int) error {
	c := line[i]
	switch {
	case p.quote != 0:
		if c == p.quote {
			p.quote = 0
		} else {
			p.cur.WriteByte(c)
		}
	case c == '"' || c == '\'':
		p.quote = c
		p.inWord = true
	case c == ' ':
		p.flush()
	case isShellByte(c) || (c == '$' && i+1 < len(line) && line[i+1] == '('):
		return ErrShell
	default:
		p.cur.WriteByte(c)
		p.inWord = true
	}
	return nil
}

func isShellByte(c byte) bool {
	return strings.IndexByte("|<>;&`", c) >= 0
}
