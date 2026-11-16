//go:build linux

package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

type githubAPIFixture struct {
	desktopAPIFixture
	old          bool
	disconnected bool
}

func (f *githubAPIFixture) GetGitHubStatus(context.Context, *kadranv1.GetGitHubStatusRequest) (*kadranv1.GetGitHubStatusResponse, error) {
	if f.old {
		return nil, status.Error(codes.Unimplemented, "method GetGitHubStatus not implemented")
	}
	return &kadranv1.GetGitHubStatusResponse{Connection: &kadranv1.GitHubConnection{
		State: kadranv1.GitHubConnection_STATE_CONNECTED,
		Login: "octo",
		Installations: []*kadranv1.GitHubInstallation{{
			Account:      "octo",
			Repositories: []*kadranv1.GitHubRepository{{FullName: "octo/site", Private: true}},
		}},
	}}, nil
}

func (f *githubAPIFixture) StartGitHubConnect(context.Context, *kadranv1.StartGitHubConnectRequest) (*kadranv1.StartGitHubConnectResponse, error) {
	return &kadranv1.StartGitHubConnectResponse{Prompt: &kadranv1.GitHubUserCodePrompt{
		UserCode:        "WDJB-MJHT",
		VerificationUri: "https://github.com/login/device",
		InstallUrl:      "https://github.com/apps/kadran-deploy/installations/new",
		ExpiresAt:       timestamppb.New(time.Unix(2000000000, 0)),
	}}, nil
}

func (f *githubAPIFixture) DisconnectGitHub(context.Context, *kadranv1.DisconnectGitHubRequest) (*kadranv1.DisconnectGitHubResponse, error) {
	f.disconnected = true
	return &kadranv1.DisconnectGitHubResponse{}, nil
}

func githubCall(t *testing.T, fixture *githubAPIFixture, method string, extra map[string]any) Response {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gh.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(managementCreds{insecure.NewCredentials()}))
	kadranv1.RegisterKadranServiceServer(server, fixture)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	var out bytes.Buffer
	d := New(Deps{CallTimeout: 5 * time.Second})
	defer d.Close()
	s := &stdio{d: d, out: &out}
	params := map[string]any{"target": "unix://" + path}
	for k, v := range extra {
		params[k] = v
	}
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	s.handleLine(context.Background(), line)
	responses := parseResponses(t, &out)
	if len(responses) != 1 {
		t.Fatalf("%s: %+v", method, responses)
	}
	return responses[0]
}

func TestGitHubMethodsReachRealGRPC(t *testing.T) {
	f := &githubAPIFixture{}
	st := githubCall(t, f, "github.status", nil)
	conn := st.Result.(map[string]any)["connection"].(map[string]any)
	if st.Error != nil || conn["state"] != "STATE_CONNECTED" || conn["login"] != "octo" {
		t.Fatalf("status: %+v", st)
	}
	connect := githubCall(t, f, "github.connect", nil)
	prompt := connect.Result.(map[string]any)["prompt"].(map[string]any)
	if connect.Error != nil || prompt["user_code"] != "WDJB-MJHT" {
		t.Fatalf("connect: %+v", connect)
	}
	if r := githubCall(t, f, "github.disconnect", map[string]any{"confirmation": "disconnect"}); r.Error != nil || !f.disconnected {
		t.Fatalf("disconnect: %+v disconnected=%v", r, f.disconnected)
	}
}

func TestGitHubOldServerErrorCarriesUnimplemented(t *testing.T) {
	r := githubCall(t, &githubAPIFixture{old: true}, "github.status", nil)
	if r.Error == nil || !strings.Contains(r.Error.Data, "code = Unimplemented") {
		t.Fatalf("expected an Unimplemented error, got %+v", r)
	}
}
