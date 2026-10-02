package main

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// protoToJSON, bir protobuf mesajını JSON'a çevirir.
//
// EmitUnpopulated açık: boş alanlar da yazılır. Tüketici (Electron, jq,
// betikler) böylece "alan yok" ile "alan boş" ayrımını yapmak zorunda
// kalmaz; şema neyse çıktı odur.
func protoToJSON(m proto.Message) (json.RawMessage, error) {
	opts := protojson.MarshalOptions{
		EmitUnpopulated: true,
		UseProtoNames:   true, // .proto'daki alan adları — şemayla birebir
	}
	b, err := opts.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("JSON'a çevrilemedi: %w", err)
	}
	return b, nil
}

// writeJSON, değeri girintili JSON olarak stdout'a yazar.
func (c *cli) writeJSON(v any) int {
	enc := json.NewEncoder(c.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return c.fail(fmt.Errorf("JSON yazılamadı: %w", err))
	}
	return exitOK
}
