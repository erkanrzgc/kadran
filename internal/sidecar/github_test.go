package sidecar

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestGitHubRejectsInvalidInputBeforeConnecting(t *testing.T) {
	for _, tc := range []struct{ method, params, want string }{
		{"github.status", `{"target":"test","token":"x"}`, "unknown parameter"},
		{"github.connect", `{"target":"test","confirmation":"disconnect"}`, "unknown parameter"},
		{"github.status", `null`, "params object"},
		{"github.status", `{"target":" "}`, "target is required"},
		{"github.disconnect", `{"target":"test"}`, `confirmation must be "disconnect"`},
		{"github.disconnect", `{"target":"test","confirmation":"Disconnect"}`, `confirmation must be "disconnect"`},
		{"github.disconnect", `{"target":"test","confirmation":"disconnect","x":1}`, "unknown parameter"},
	} {
		t.Run(tc.method+tc.params, func(t *testing.T) {
			d := New(Deps{})
			_, rpcErr := d.Dispatch(context.Background(), tc.method, json.RawMessage(tc.params))
			if rpcErr == nil || rpcErr.Code != CodeInvalidParams || !strings.Contains(rpcErr.Message, tc.want) {
				t.Fatalf("expected invalid params %q before SSH, got %+v", tc.want, rpcErr)
			}
		})
	}
}

func TestGitHubDisconnectAcceptsOnlyTheLiteralWord(t *testing.T) {
	if _, err := decodeGitHub("github.disconnect", json.RawMessage(`{"target":"t","confirmation":"disconnect"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeGitHub("github.status", json.RawMessage(`{"target":"t"}`)); err != nil {
		t.Fatal(err)
	}
}
