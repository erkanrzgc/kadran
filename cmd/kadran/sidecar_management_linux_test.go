//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/erkanrzgc/kadran/internal/client"
	"github.com/erkanrzgc/kadran/internal/connproto"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/version"
)

type managementCreds struct {
	credentials.TransportCredentials
}

func (c managementCreds) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if _, err := connproto.Read(conn); err != nil {
		return nil, nil, err
	}
	return c.TransportCredentials.ServerHandshake(conn)
}

type desktopAPIFixture struct {
	kadranv1.UnimplementedKadranServiceServer
	app    *kadranv1.App
	commit string
	tail   uint32
}

func (f *desktopAPIFixture) Ping(context.Context, *kadranv1.PingRequest) (*kadranv1.PingResponse, error) {
	return &kadranv1.PingResponse{ProtocolVersion: version.Protocol}, nil
}
func (f *desktopAPIFixture) ListApps(context.Context, *kadranv1.ListAppsRequest) (*kadranv1.ListAppsResponse, error) {
	return &kadranv1.ListAppsResponse{Apps: []*kadranv1.App{f.app}}, nil
}
func (f *desktopAPIFixture) CreateApp(_ context.Context, r *kadranv1.CreateAppRequest) (*kadranv1.CreateAppResponse, error) {
	f.app = &kadranv1.App{Spec: r.Spec}
	return &kadranv1.CreateAppResponse{App: f.app}, nil
}
func (f *desktopAPIFixture) UpdateApp(_ context.Context, r *kadranv1.UpdateAppRequest) (*kadranv1.UpdateAppResponse, error) {
	f.app.Spec.Domain = r.GetDomain()
	return &kadranv1.UpdateAppResponse{App: f.app}, nil
}
func (f *desktopAPIFixture) GetApp(context.Context, *kadranv1.GetAppRequest) (*kadranv1.GetAppResponse, error) {
	active := "r1"
	return &kadranv1.GetAppResponse{App: f.app, ActiveReleaseId: &active}, nil
}
func (f *desktopAPIFixture) Deploy(r *kadranv1.DeployRequest, s grpc.ServerStreamingServer[kadranv1.DeployResponse]) error {
	f.commit = r.CommitSha
	for _, event := range []*kadranv1.DeployResponse{
		{Event: &kadranv1.DeployResponse_Accepted{Accepted: &kadranv1.DeployAccepted{ReleaseId: "r1", AppId: r.AppId}}},
		{Event: &kadranv1.DeployResponse_Output{Output: &kadranv1.BuildOutput{Data: []byte("İstanbul 🧊")}}},
		{Event: &kadranv1.DeployResponse_Succeeded{Succeeded: &kadranv1.DeploySucceeded{ReleaseId: "r1", ImageId: "sha256:abc"}}},
	} {
		if err := s.Send(event); err != nil {
			return err
		}
	}
	return nil
}
func (f *desktopAPIFixture) StreamLogs(r *kadranv1.StreamLogsRequest, s grpc.ServerStreamingServer[kadranv1.StreamLogsResponse]) error {
	f.tail = r.TailLines
	if r.Follow {
		panic("desktop must not follow unbounded logs")
	}
	return s.Send(&kadranv1.StreamLogsResponse{Data: []byte("log İ\n")})
}
func (f *desktopAPIFixture) Rollback(_ context.Context, r *kadranv1.RollbackRequest) (*kadranv1.RollbackResponse, error) {
	return &kadranv1.RollbackResponse{AppId: r.AppId, FromReleaseId: "r1", ToReleaseId: "r0"}, nil
}
func (f *desktopAPIFixture) DeleteApp(_ context.Context, r *kadranv1.DeleteAppRequest) (*kadranv1.DeleteAppResponse, error) {
	return &kadranv1.DeleteAppResponse{AppId: r.AppId, VolumesKept: []string{"data"}}, nil
}
func (f *desktopAPIFixture) ListAlarms(context.Context, *kadranv1.ListAlarmsRequest) (*kadranv1.ListAlarmsResponse, error) {
	return &kadranv1.ListAlarmsResponse{DeliveryIsLocalOnly: true}, nil
}
func (f *desktopAPIFixture) ListBackups(context.Context, *kadranv1.ListBackupsRequest) (*kadranv1.ListBackupsResponse, error) {
	return &kadranv1.ListBackupsResponse{Keep: 7}, nil
}
func (f *desktopAPIFixture) CreateBackup(context.Context, *kadranv1.CreateBackupRequest) (*kadranv1.CreateBackupResponse, error) {
	return &kadranv1.CreateBackupResponse{Backup: &kadranv1.BackupInfo{Path: "/backup/test.db", Bytes: 1024}, VolumesExcluded: true}, nil
}

func TestDesktopManagementRealUnixGRPCJourney(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &desktopAPIFixture{}
	server := grpc.NewServer(grpc.Creds(managementCreds{insecure.NewCredentials()}))
	kadranv1.RegisterKadranServiceServer(server, fixture)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	var out bytes.Buffer
	s := &sidecar{cli: &cli{stdout: &out, stderr: &bytes.Buffer{}}, callTimeout: time.Second * 5, conns: map[string]*client.Client{}}
	defer s.closeAll()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()
	sha := "1111111111111111111111111111111111111111"
	calls := []struct {
		method string
		params map[string]any
		field  string
		want   any
	}{
		{"apps.create", map[string]any{"spec": map[string]any{"app_id": "blog", "git_host": "github.com", "git_owner": "o", "git_repo": "r"}}, "app", nil},
		{"apps.update", map[string]any{"app_id": "blog", "update": map[string]any{"domain": "new.example.com"}}, "app", nil},
		{"apps.list", nil, "apps", nil},
		{"apps.get", map[string]any{"app_id": "blog"}, "active_release_id", "r1"},
		{"apps.deploy", map[string]any{"app_id": "blog", "ref": sha}, "output", "İstanbul 🧊"},
		{"apps.logs", map[string]any{"app_id": "blog", "tail": 100}, "output", "log İ\n"},
		{"apps.rollback", map[string]any{"app_id": "blog", "confirmation": "blog"}, "to_release_id", "r0"},
		{"alarms.list", nil, "delivery_is_local_only", true},
		{"backups.list", nil, "keep", float64(7)},
		{"backups.create", nil, "volumes_excluded", true},
		{"apps.delete", map[string]any{"app_id": "blog", "confirmation": "blog"}, "volumes_kept", nil},
	}
	for i, call := range calls {
		params := call.params
		if params == nil {
			params = map[string]any{}
		}
		params["target"] = "unix://" + path
		line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": i + 1, "method": call.method, "params": params})
		out.Reset()
		s.handleLine(ctx, line)
		responses := parseResponses(t, &out)
		if len(responses) != 1 || responses[0].Error != nil {
			t.Fatalf("%s: %+v", call.method, responses)
		}
		result, ok := responses[0].Result.(map[string]any)
		if !ok {
			t.Fatalf("%s wrong shape", call.method)
		}
		value, exists := result[call.field]
		if !exists || (call.want != nil && value != call.want) {
			t.Fatalf("%s: %s=%v", call.method, call.field, value)
		}
	}
	if fixture.commit != sha || fixture.tail != 100 || fixture.app.Spec.Domain != "new.example.com" {
		t.Fatal("management fields did not reach actual gRPC")
	}
}
