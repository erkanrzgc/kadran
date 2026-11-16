package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// githubDisconnectWord, github.disconnect'in beklediği onay sözcüğü.
// Masaüstü köprüsü de aynısını ister; burada ikinci savunma hattı.
const githubDisconnectWord = "disconnect"

type githubParams struct {
	Target       string `json:"target"`
	Confirmation string `json:"confirmation,omitempty"`
}

// decodeGitHub, github.* yöntemlerinin parametrelerini çözer. Her yöntemin
// kendi alan listesi var; bilinmeyen alan SSH'a hiç ulaşmaz.
func decodeGitHub(method string, params json.RawMessage) (githubParams, error) {
	var p githubParams
	if len(params) == 0 || bytes.Equal(bytes.TrimSpace(params), []byte("null")) {
		return p, errors.New("params object is required")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(params, &fields); err != nil {
		return p, err
	}
	allowed := map[string]bool{"target": true}
	if method == "github.disconnect" {
		allowed["confirmation"] = true
	}
	for field := range fields {
		if !allowed[field] {
			return p, fmt.Errorf("unknown parameter %q", field)
		}
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return p, err
	}
	if strings.TrimSpace(p.Target) == "" {
		return p, errors.New("target is required")
	}
	if method == "github.disconnect" && p.Confirmation != githubDisconnectWord {
		return p, errors.New(`confirmation must be "disconnect"`)
	}
	return p, nil
}

// dispatchGitHub, GitHub bağlantısının üç yöntemi (K-167). Hepsi yalnız
// yöneticiye açık kadrand çağrılarıdır; eski sunucu Unimplemented döner ve
// hata metni bunu taşır (arayüz "sunucuyu yükseltin" der).
func (d *Dispatcher) dispatchGitHub(ctx context.Context, method string, params json.RawMessage) (any, *Error) {
	if _, err := decodeGitHub(method, params); err != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
	}
	return d.withConn(ctx, params, func(ctx context.Context, conn *client.Client) (any, error) {
		rpc := conn.RPC()
		switch method {
		case "github.status":
			resp, err := rpc.GetGitHubStatus(ctx, &kadranv1.GetGitHubStatusRequest{})
			if err != nil {
				return nil, err
			}
			return ProtoToJSON(resp)
		case "github.connect":
			resp, err := rpc.StartGitHubConnect(ctx, &kadranv1.StartGitHubConnectRequest{})
			if err != nil {
				return nil, err
			}
			return ProtoToJSON(resp)
		case "github.disconnect":
			resp, err := rpc.DisconnectGitHub(ctx, &kadranv1.DisconnectGitHubRequest{})
			if err != nil {
				return nil, err
			}
			return ProtoToJSON(resp)
		}
		return nil, errors.New("unknown github method")
	})
}
