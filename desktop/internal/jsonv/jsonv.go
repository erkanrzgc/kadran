// Package jsonv, arayüzden gelen JSON'u alan SIRASINI koruyarak çözer ve
// aynı sırayla yeniden yazar.
//
// # Neden encoding/json'un map'i değil?
//
// Onay kutusu ortam değişkeni ve derleme argümanı adlarını kullanıcının
// yazdığı sırayla gösterir (Electron'da JavaScript nesnesinin sırası).
// Go haritası sırasızdır: aynı yük iki açılışta farklı görünürdü. Ayrıca
// sayılar json.Number olarak kalır: "3000" metni ile 3000 sayısı ayrı
// tutulur ve sunucuya yazıldığı biçimde gider (protojson ikisini de kabul
// eder).
//
// Değerler: nil, bool, json.Number, string, []any, *Object.
package jsonv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Object, sırası korunmuş bir JSON nesnesi. Aynı anahtar iki kez gelirse
// JavaScript'teki gibi son değer kazanır, yeri ilk geldiği yerdir.
type Object struct {
	keys   []string
	values map[string]any
}

// NewObject, boş bir nesne.
func NewObject() *Object { return &Object{values: map[string]any{}} }

// Keys, anahtarlar geliş sırasıyla.
func (o *Object) Keys() []string { return append([]string(nil), o.keys...) }

// Len, anahtar sayısı.
func (o *Object) Len() int { return len(o.keys) }

// Get, anahtarın değeri; yoksa ok false.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.values[key]
	return v, ok
}

// Has, anahtar var mı (değeri null olsa da).
func (o *Object) Has(key string) bool {
	_, ok := o.values[key]
	return ok
}

// Set, anahtarı ekler ya da değerini değiştirir.
func (o *Object) Set(key string, value any) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// Decode, tek bir JSON değeri çözer; arkasında başka veri varsa hata.
func Decode(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewObject()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			list := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return list, nil
		}
		return nil, fmt.Errorf("unexpected %q", t)
	default:
		return tok, nil
	}
}

// Encode, değeri sırasını koruyarak yazar. HTML karakterleri kaçışlanmaz:
// metin sunucuya olduğu gibi gider.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeValue(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case *Object:
		buf.WriteByte('{')
		for i, k := range t.keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encodeScalar(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := encodeValue(buf, t.values[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case []any:
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encodeValue(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case nil, bool, json.Number, string:
		return encodeScalar(buf, t)
	default:
		return fmt.Errorf("unsupported JSON value %T", v)
	}
}

func encodeScalar(buf *bytes.Buffer, v any) error {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	buf.Truncate(buf.Len() - 1) // Encode'un eklediği satır sonu
	return nil
}
