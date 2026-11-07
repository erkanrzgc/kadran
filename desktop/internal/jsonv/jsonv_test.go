package jsonv

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRoundTripKeepsOrderAndNumberText(t *testing.T) {
	in := `{"z":1,"a":"3000","m":{"y":[1,"x",null,true],"b":1e3},"html":"<a&b>"}`
	v, err := Decode([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := v.(*Object)
	if !ok {
		t.Fatalf("nesne değil: %T", v)
	}
	if got := strings.Join(obj.Keys(), ","); got != "z,a,m,html" {
		t.Fatalf("sıra: %s", got)
	}
	if n, _ := obj.Get("z"); n != json.Number("1") {
		t.Fatalf("sayı json.Number değil: %#v", n)
	}
	out, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Fatalf("yeniden yazım farklı:\n%s\n%s", out, in)
	}
}

func TestDuplicateKeyLastValueFirstPlace(t *testing.T) {
	v, err := Decode([]byte(`{"a":1,"b":2,"a":3}`))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := Encode(v)
	if string(out) != `{"a":3,"b":2}` {
		t.Fatalf("got %s", out)
	}
}

func TestRejectsTrailingDataAndGarbage(t *testing.T) {
	for _, in := range []string{`{"a":1} {}`, `{"a":`, `[1,]`, ``, `{1:2}`} {
		if _, err := Decode([]byte(in)); err == nil {
			t.Errorf("%q kabul edildi", in)
		}
	}
}

func TestEncodeRejectsForeignTypes(t *testing.T) {
	if _, err := Encode(map[string]any{"a": 1}); err == nil {
		t.Fatal("Go haritası sessizce yazıldı")
	}
}
