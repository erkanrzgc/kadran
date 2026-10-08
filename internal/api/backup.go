package api

import (
	"context"
	"path/filepath"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/store"
)

// CreateBackup, veritabanının anlık bir yedeğini alır.
//
// ── Neden denetim zincirine giriyor ─────────────────────────────────
//
// Salt okunur bir işlem gibi görünüyor ama değil: diskte, veritabanının
// TAMAMINI taşıyan yeni bir dosya üretiyor — ortam değişkenleri ve
// denetim zinciri dahil. Kim ne zaman kopya çıkardı sorusunun cevabı
// zincirde durmalı.
//
// Yedek DOSYASININ kendisi zinciri de içeriyor, yani bir kopyayı ele
// geçiren geçmişi de görür. Bu, yedeklerin 0700 bir dizinde durmasının
// sebebi (snapshot.go).
func (s *Server) CreateBackup(
	ctx context.Context, _ *kadranv1.CreateBackupRequest,
) (*kadranv1.CreateBackupResponse, error) {
	const action = "backup.create"
	const target = "kadran.db"

	snap, err := s.store.Snapshot(ctx)
	if err != nil {
		if rerr := s.completed(ctx, action, target, nil, err); rerr != nil {
			return nil, rerr
		}
		return nil, status.Errorf(codes.Internal, "could not take a backup: %v", err)
	}

	// Dosya ADI kayda giriyor, tam yol değil: dizin sunucu
	// yapılandırmasıdır ve zincire yazılacak bir bilgi taşımaz.
	params := map[string]string{
		"dosya": filepath.Base(snap.Path),
		"bayt":  strconv.FormatInt(snap.Bytes, 10),
	}
	if err := s.completed(ctx, action, target, params, nil); err != nil {
		return nil, err
	}

	return &kadranv1.CreateBackupResponse{
		Backup: snapshotToProto(snap),
		// Sabit true. Gerekçe api.proto'daki alan yorumunda: kullanıcı
		// "yedek aldım" deyince her şeyin yedeklendiğini varsayar.
		VolumesExcluded: true,
	}, nil
}

// ListBackups, diskteki yedekleri listeler.
//
// Salt okunur; zincire girmiyor (record.go'daki gerekçe: durum okuma
// gürültüsü, durum değiştiren işlemleri görünmez kılardı).
func (s *Server) ListBackups(
	_ context.Context, _ *kadranv1.ListBackupsRequest,
) (*kadranv1.ListBackupsResponse, error) {
	snaps, err := s.store.ListSnapshots()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not list backups: %v", err)
	}

	out := make([]*kadranv1.BackupInfo, 0, len(snaps))
	for _, sn := range snaps {
		out = append(out, snapshotToProto(sn))
	}
	return &kadranv1.ListBackupsResponse{
		Backups: out,
		Keep:    store.SnapshotKeep,
	}, nil
}

func snapshotToProto(sn store.SnapshotInfo) *kadranv1.BackupInfo {
	return &kadranv1.BackupInfo{
		Path:      sn.Path,
		Bytes:     sn.Bytes,
		TakenUnix: sn.Taken.Unix(),
	}
}
