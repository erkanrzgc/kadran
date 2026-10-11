package main

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

func TestManagementRejectsInvalidInputBeforeConnecting(t *testing.T) {
	for _, tc := range []struct{ method, params string }{
		{"apps.get", `{"target":"test","app_id":"../other"}`},
		{"apps.deploy", `{"target":"test","app_id":"blog","ref":"-option"}`},
		{"apps.delete", `{"target":"test","app_id":"blog","confirmation":"other"}`},
		{"apps.rollback", `{"target":"test","app_id":"blog"}`},
		{"apps.logs", `{"target":"test","app_id":"blog","tail":10001}`},
		{"apps.create", `{"target":"test","spec":{"app_id":"blog","token":"secret"}}`},
		{"apps.update", `{"target":"test","app_id":"blog","update":{"container_port":100}}`},
		{"apps.list", `{"target":"test","command":"sh"}`},
		{"backups.create", `null`},
	} {
		t.Run(tc.method+tc.params, func(t *testing.T) {
			s := &sidecar{}
			_, rpcErr := s.dispatch(context.Background(), rpcRequest{Method: tc.method, Params: json.RawMessage(tc.params)})
			if rpcErr == nil || rpcErr.Code != codeInvalidParams {
				t.Fatalf("expected invalid params before SSH, got %+v", rpcErr)
			}
		})
	}
}

func TestDesktopDeployRequiresFinalSuccess(t *testing.T) {
	accepted := &kadranv1.DeployResponse{Event: &kadranv1.DeployResponse_Accepted{Accepted: &kadranv1.DeployAccepted{AppId: "blog", ReleaseId: "r1"}}}
	output := &kadranv1.DeployResponse{Event: &kadranv1.DeployResponse_Output{Output: &kadranv1.BuildOutput{Data: []byte("İstanbul")}}}
	success := &kadranv1.DeployResponse{Event: &kadranv1.DeployResponse_Succeeded{Succeeded: &kadranv1.DeploySucceeded{ReleaseId: "r1", ImageId: "sha256:abc"}}}
	for _, tc := range []struct {
		name   string
		events []*kadranv1.DeployResponse
		ok     bool
	}{
		{"logs then EOF", []*kadranv1.DeployResponse{accepted, output}, false},
		{"positive final event", []*kadranv1.DeployResponse{accepted, output, success}, true},
		{"output after success", []*kadranv1.DeployResponse{accepted, success, output}, false},
		{"success without acceptance", []*kadranv1.DeployResponse{success}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := 0
			result, err := collectDesktopDeploy(func() (*kadranv1.DeployResponse, error) {
				if i == len(tc.events) {
					return nil, io.EOF
				}
				e := tc.events[i]
				i++
				return e, nil
			})
			if (err == nil) != tc.ok {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if tc.ok && result.Output != "İstanbul" {
				t.Fatal("output lost")
			}
		})
	}
}

func TestDesktopOutputIsBoundedAndPreservesUTF8(t *testing.T) {
	var out boundedOutput
	text := strings.Repeat("x", maxDesktopOutput-1) + "İ"
	out.Write([]byte(text))
	out.Write([]byte("more"))
	if !out.truncated || len(out.String()) > maxDesktopOutput || strings.Contains(out.String(), "�") {
		t.Fatalf("output boundary broken: %d", len(out.String()))
	}
}
