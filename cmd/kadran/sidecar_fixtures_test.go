package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// sidecarFixtures, masaüstünün sahte sidecar'ının döndürdüğü yanıtlar.
//
// Biçim elle yazılmıyor: dosyalar protoToJSON'ın GERÇEK çıktısı. Proto
// değişip dosyalar yenilenmezse bu test düşer ve masaüstünün sahte
// sunucuya karşı geçen testleri gerçeğinden ayrışmaz. Yenilemek için:
//
//	KADRAN_UPDATE_FIXTURES=1 go test ./cmd/kadran -run TestSidecarFixtures
var sidecarFixtures = map[string]proto.Message{
	"status.json": &kadranv1.GetSystemInfoResponse{
		DaemonVersion:       "0.5.1",
		Hostname:            "kadran-nbg1",
		DaemonUptimeSeconds: 371_520,
		RunningAsUser:       "kadran",
		ExecutorReachable:   false,
		Host: &kadranv1.HostInfo{
			Os:                   "linux",
			KernelVersion:        "6.12.48",
			Architecture:         "amd64",
			CpuCount:             2,
			MemoryTotalBytes:     3_970_000_000,
			MemoryAvailableBytes: 1_720_000_000,
			DockerVersion:        "28.4.0",
			DiskTotalBytes:       40_000_000_000,
			DiskAvailableBytes:   22_000_000_000,
		},
	},
	"verify.json": &kadranv1.VerifyAuditChainResponse{
		DaemonStatus:   kadranv1.ChainStatus_CHAIN_STATUS_VALID,
		RecordsChecked: 1284,
		ExecutorStatus: kadranv1.ChainStatus_CHAIN_STATUS_UNREACHABLE,
		ExecutorDetail: "executor unreachable: dial unix /run/kadran/exec.sock: connect: no such file or directory",
	},
	// Sahte sidecar her çağrıda sayaçları bu tabandan artırır (K-166).
	"resources.json": &kadranv1.GetResourceUsageResponse{
		SampledAtUnixMs: 1_791_633_600_000,
		AppsMeasured:    true,
		Host: &kadranv1.HostUsage{
			CpuCount:             2,
			CpuTotalTicks:        proto.Uint64(74_304_000),
			CpuIdleTicks:         proto.Uint64(70_100_000),
			CpuIowaitTicks:       proto.Uint64(410_000),
			CpuStealTicks:        proto.Uint64(52_000),
			MemoryTotalBytes:     proto.Uint64(3_970_000_000),
			MemoryAvailableBytes: proto.Uint64(1_720_000_000),
			SwapTotalBytes:       proto.Uint64(0),
			SwapFreeBytes:        proto.Uint64(0),
			Load1:                proto.Float64(0.42),
			Load5:                proto.Float64(0.31),
			Load15:               proto.Float64(0.25),
			Tasks:                proto.Uint32(187),
			DiskTotalBytes:       proto.Uint64(40_000_000_000),
			DiskAvailableBytes:   proto.Uint64(22_000_000_000),
			InodesTotal:          proto.Uint64(2_621_440),
			InodesFree:           proto.Uint64(2_480_112),
			DiskReadBytes:        proto.Uint64(9_120_000_000),
			DiskWriteBytes:       proto.Uint64(31_400_000_000),
			NetRxBytes:           proto.Uint64(48_200_000_000),
			NetTxBytes:           proto.Uint64(12_700_000_000),
			CpuPressure:          proto.Float64(0.5),
			MemoryPressure:       proto.Float64(0),
			IoPressure:           proto.Float64(1.25),
			UptimeSeconds:        proto.Uint64(371_520),
		},
		Apps: []*kadranv1.AppUsage{
			{AppId: "hello", Containers: 1, CpuUsageNs: 5_400_000_000_000, MemoryBytes: 88_000_000, NetRxBytes: 920_000_000, NetTxBytes: 4_100_000_000, DiskReadBytes: 12_000_000, DiskWriteBytes: 0, Tasks: 9},
			{AppId: "worker", Containers: 2, CpuUsageNs: 910_000_000_000, MemoryBytes: 214_000_000, MemoryLimitBytes: 536_870_912, NetRxBytes: 31_000_000, NetTxBytes: 2_000_000, DiskReadBytes: 0, DiskWriteBytes: 640_000_000, Tasks: 24},
		},
	},
	"audit-record.json": &kadranv1.AuditRecord{
		Seq: 1,
		Ts:  timestamppb.New(time.Date(2026, 10, 6, 18, 42, 10, 0, time.UTC)),
		Actor: &kadranv1.Actor{
			SshKeyFingerprint: "SHA256:4f1c2b7d9e0a11223344556677889900aabbccddeeffa9e2",
			Origin:            "ssh",
		},
		Action:   "deploy",
		Target:   "hello",
		Outcome:  kadranv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS,
		PrevHash: []byte{1, 2, 3},
		Hash:     []byte{4, 5, 6},
		Source:   kadranv1.AuditSource_AUDIT_SOURCE_DAEMON,
	},
}

func TestSidecarFixtures(t *testing.T) {
	dir := filepath.Join("..", "..", "desktop", "test", "fixtures")
	update := os.Getenv("KADRAN_UPDATE_FIXTURES") == "1"

	for name, msg := range sidecarFixtures {
		raw, err := protoToJSON(msg)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// protojson boşlukları bilerek rastgeleleştiriyor; bayt bayt
		// karşılaştırma kararsız olurdu. Anlam karşılaştırılıyor.
		var got any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		path := filepath.Join(dir, name)
		if update {
			body, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}

		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s okunamadı (KADRAN_UPDATE_FIXTURES=1 ile üret): %v", name, err)
		}
		var want any
		if err := json.Unmarshal(body, &want); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s protoToJSON çıktısıyla uyuşmuyor; proto değişmiş olabilir, KADRAN_UPDATE_FIXTURES=1 ile yenile", name)
		}
	}
}
