package main

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/erkanrzgc/kadran/internal/sidecar"
)

// protoToJSON, bir protobuf mesajını JSON'a çevirir. Sidecar ile aynı
// biçim (boş alanlar da yazılır, .proto alan adları): tek kaynak orada.
func protoToJSON(m proto.Message) (json.RawMessage, error) {
	return sidecar.ProtoToJSON(m)
}

// writeJSON, değeri girintili JSON olarak stdout'a yazar.
func (c *cli) writeJSON(v any) int {
	enc := json.NewEncoder(c.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return c.fail(fmt.Errorf("could not write JSON: %w", err))
	}
	return exitOK
}
