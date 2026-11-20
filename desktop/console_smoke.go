//go:build windows && smoke

package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/version"
)

// Konsolun duman testi düzeneği (yalnız `-tags smoke`; dağıtılan ikiliye
// GİRMEZ, karşılığı console_release.go).
//
//   - Sahte kadrand: bufconn üstünde gerçek gRPC sunucusu; konsolun
//     bağlantı önbelleği (Conns) bu sahteyle kurulur, ssh ve sunucu yok.
//   - Sahte Host: konsolun onay kutusunu kaydeder ve onaylar. Duman bayrağı
//     (Fixture) konsol kutusunu ATLATMAZ: kutunun istendiği buradan, kayıttan
//     kanıtlanır (yolculuk `boxes` komutuyla okur).

// smokeSecret, sahte TOTP sırrı: duman testi bu dizenin kart dışında hiçbir
// yerde görünmediğini sınar.
const smokeSecret = "JBSWY3DPEHPK3PXP"

// smokeRecoveryCodes, sahte kurtarma kodları (`aaaa-bbbb` biçimi).
var smokeRecoveryCodes = []string{"aaaa-bbbb", "cccc-dddd"}

const (
	smokeStepDelay   = 40 * time.Millisecond
	smokeFollowEvery = 120 * time.Millisecond
	smokeApp         = "web"
)

// fakeKadrand, konsol senaryolarının dokunduğu RPC'leri yanıtlar.
type fakeKadrand struct {
	kadranv1.UnimplementedKadranServiceServer
}

func (fakeKadrand) Ping(context.Context, *kadranv1.PingRequest) (*kadranv1.PingResponse, error) {
	return &kadranv1.PingResponse{
		DaemonVersion:   "smoke",
		ProtocolVersion: version.Protocol,
		ServerTime:      timestamppb.Now(),
		TotpRequired:    proto.Bool(false),
	}, nil
}

func smokeAppRecord() *kadranv1.App {
	return &kadranv1.App{
		Spec: &kadranv1.AppSpec{
			AppId: smokeApp, GitHost: "github.com", GitOwner: "demo", GitRepo: smokeApp,
			GitBranch: "main", ContainerPort: 8080, Replicas: 1,
		},
		ReleaseCount: 3,
	}
}

func (fakeKadrand) ListApps(context.Context, *kadranv1.ListAppsRequest) (*kadranv1.ListAppsResponse, error) {
	return &kadranv1.ListAppsResponse{Apps: []*kadranv1.App{smokeAppRecord()}}, nil
}

func (fakeKadrand) GetApp(context.Context, *kadranv1.GetAppRequest) (*kadranv1.GetAppResponse, error) {
	return &kadranv1.GetAppResponse{
		App:             smokeAppRecord(),
		ActiveReleaseId: proto.String("r3"),
		Releases:        []*kadranv1.Release{{ReleaseId: "r3", AppId: smokeApp, Status: kadranv1.ReleaseStatus_RELEASE_STATUS_BUILT}},
	}, nil
}

// Deploy: kabul + üç derleme satırı + başarı; satırlar akıyormuş gibi aralıklı.
func (fakeKadrand) Deploy(req *kadranv1.DeployRequest, stream grpc.ServerStreamingServer[kadranv1.DeployResponse]) error {
	send := func(r *kadranv1.DeployResponse) error {
		time.Sleep(smokeStepDelay)
		return stream.Send(r)
	}
	if err := send(&kadranv1.DeployResponse{Event: &kadranv1.DeployResponse_Accepted{Accepted: &kadranv1.DeployAccepted{
		ReleaseId: "r4", AppId: req.GetAppId(), CommitSha: req.GetCommitSha(),
	}}}); err != nil {
		return err
	}
	for i := 1; i <= 3; i++ {
		line := fmt.Sprintf("build step %d/3\n", i)
		if err := send(&kadranv1.DeployResponse{Event: &kadranv1.DeployResponse_Output{Output: &kadranv1.BuildOutput{Data: []byte(line)}}}); err != nil {
			return err
		}
	}
	return send(&kadranv1.DeployResponse{Event: &kadranv1.DeployResponse_Succeeded{Succeeded: &kadranv1.DeploySucceeded{
		ReleaseId: "r4", ImageId: "sha256:smoke",
	}}})
}

// StreamLogs: tail kadar geçmiş satır (belleği doldurmak için büyük olabilir),
// -f ise istek iptal edilene dek her 120 ms'de bir satır.
func (fakeKadrand) StreamLogs(req *kadranv1.StreamLogsRequest, stream grpc.ServerStreamingServer[kadranv1.StreamLogsResponse]) error {
	for i := 1; i <= int(req.GetTailLines()); i++ {
		line := fmt.Sprintf("history line %d\n", i)
		if err := stream.Send(&kadranv1.StreamLogsResponse{Data: []byte(line)}); err != nil {
			return err
		}
	}
	if !req.GetFollow() {
		return nil
	}
	tick := time.NewTicker(smokeFollowEvery)
	defer tick.Stop()
	for n := 1; ; n++ {
		select {
		case <-stream.Context().Done():
			return nil
		case <-tick.C:
			if err := stream.Send(&kadranv1.StreamLogsResponse{Data: []byte(fmt.Sprintf("live line %d\n", n))}); err != nil {
				return err
			}
		}
	}
}

func (fakeKadrand) BeginTotpEnrollment(context.Context, *kadranv1.BeginTotpEnrollmentRequest) (*kadranv1.BeginTotpEnrollmentResponse, error) {
	return &kadranv1.BeginTotpEnrollmentResponse{
		OtpauthUri:   "otpauth://totp/Kadran:smoke?secret=" + smokeSecret + "&issuer=Kadran",
		SecretBase32: smokeSecret,
		ExpiresAt:    timestamppb.New(time.Now().Add(5 * time.Minute)),
	}, nil
}

func (fakeKadrand) ConfirmTotpEnrollment(context.Context, *kadranv1.ConfirmTotpEnrollmentRequest) (*kadranv1.ConfirmTotpEnrollmentResponse, error) {
	return &kadranv1.ConfirmTotpEnrollmentResponse{
		RecoveryCodes: smokeRecoveryCodes,
		SessionToken:  "smoke-session",
		ExpiresAt:     timestamppb.New(time.Now().Add(8 * time.Hour)),
	}, nil
}

// consoleDial, konsolun bağlantı kurucusu: bufconn üstündeki sahte kadrand.
// Hedef yok sayılır; her sunucu aynı sahteye gider.
func consoleDial() func(client.Target) (*client.Client, error) {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	kadranv1.RegisterKadranServiceServer(srv, fakeKadrand{})
	go func() { _ = srv.Serve(lis) }()
	return func(t client.Target) (*client.Client, error) {
		return client.DialWithDialer(t, func(context.Context, string) (net.Conn, error) { return lis.Dial() })
	}
}

// boxLog, konsolun istediği onay kutuları (sırayla).
var boxLog struct {
	mu   sync.Mutex
	list []confirm.Box
}

// consoleConfirm, sahte Host'un kutusu: kaydeder, onaylar.
func consoleConfirm(*host) func(confirm.Box) (bool, error) {
	return func(b confirm.Box) (bool, error) {
		boxLog.mu.Lock()
		boxLog.list = append(boxLog.list, b)
		boxLog.mu.Unlock()
		return true, nil
	}
}

// consoleBoxes, şu ana dek istenen kutuların kopyası.
func consoleBoxes() []confirm.Box {
	boxLog.mu.Lock()
	defer boxLog.mu.Unlock()
	return append([]confirm.Box{}, boxLog.list...)
}
