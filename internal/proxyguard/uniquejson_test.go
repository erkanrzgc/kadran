package proxyguard

import (
	"strings"
	"testing"
)

func TestUniqueJSONRejectsExcessiveNesting(t *testing.T) {
	deep := strings.Repeat("[", maxJSONDepth+1) + strings.Repeat("]", maxJSONDepth+1)
	if err := uniqueJSON([]byte(deep)); err == nil {
		t.Fatal("document nested beyond the limit was accepted")
	}
	// Çok daha derin bir belge de yığını patlatmadan reddedilmeli.
	huge := strings.Repeat("[", 1_000_000)
	if err := uniqueJSON([]byte(huge)); err == nil {
		t.Fatal("pathologically nested document was accepted")
	}
}

func TestUniqueJSONAcceptsNestingAtTheLimit(t *testing.T) {
	ok := strings.Repeat("[", maxJSONDepth) + strings.Repeat("]", maxJSONDepth)
	if err := uniqueJSON([]byte(ok)); err != nil {
		t.Fatalf("document at the depth limit was rejected: %v", err)
	}
}
