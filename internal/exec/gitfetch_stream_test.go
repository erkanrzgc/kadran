package exec

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// paceStream, parçaları belirli aralıkla veren sahte akıştır (soket yok,
// Windows'ta da koşar). Bağlam iptal edilince Recv hata döner; gerçek gRPC
// akışı da böyle davranır.
type paceStream struct {
	grpc.ClientStream
	ctx    context.Context
	chunks [][]byte
	gap    time.Duration
}

func (p *paceStream) Recv() (*kadranv1.FetchArchiveResponse, error) {
	if len(p.chunks) == 0 {
		return nil, io.EOF
	}
	select {
	case <-time.After(p.gap):
	case <-p.ctx.Done():
		return nil, p.ctx.Err()
	}
	c := p.chunks[0]
	p.chunks = p.chunks[1:]
	return &kadranv1.FetchArchiveResponse{Kind: &kadranv1.FetchArchiveResponse_Chunk{Chunk: c}}, nil
}

func smallGzTar(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	body := []byte("FROM scratch\n")
	for _, h := range []*tar.Header{
		{Name: "r/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "r/Dockerfile", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// Boşta süresi HER parçada sıfırlanır: toplam süre idle penceresini (600 ms)
// aşsa da (7 parça x 200 ms ≈ 1,4 sn) parçalar aralıksız geldiği sürece
// indirme başarılıdır. Sıfırlama kalkarsa 600. ms'de akış "stalled" diye
// kesilirdi. Pay 3x: yavaş bir CI'da tek bir gecikmiş parça testi düşürmesin.
func TestArchiveSlowButSteadyStreamSucceeds(t *testing.T) {
	const (
		idle = 600 * time.Millisecond
		gap  = 200 * time.Millisecond
	)
	data := smallGzTar(t)
	const pieces = 7
	var chunks [][]byte
	step := (len(data) + pieces - 1) / pieces
	for len(data) > 0 {
		n := min(step, len(data))
		chunks = append(chunks, data[:n])
		data = data[n:]
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := &paceStream{ctx: ctx, chunks: chunks, gap: gap}

	start := time.Now()
	f, err := receiveArchive(ctx, st, cancel, idle)
	if err != nil || f == nil {
		t.Fatalf("steady slow stream failed: %v", err)
	}
	defer closePrivateContext(f)
	if len(chunks) < 4 || time.Since(start) <= idle {
		t.Fatalf("stream finished in %v: the fixture no longer outlasts the idle window", time.Since(start))
	}
}
