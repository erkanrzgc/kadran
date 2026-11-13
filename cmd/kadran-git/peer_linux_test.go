//go:build linux

package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// serve verilen sunucuyu gerçek bir unix soketinde başlatır ve yolunu döner.
func serve(t *testing.T, s *grpc.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = s.Serve(ln) }()
	t.Cleanup(s.Stop)
	return path
}

func dial(t *testing.T, path string) *grpc.ClientConn {
	t.Helper()
	cc, err := grpc.NewClient("unix://"+path, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	return cc
}

func callStatus(t *testing.T, path string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := kadranv1.NewGitAdminServiceClient(dial(t, path)).GetGitStatus(ctx, &kadranv1.GetGitStatusRequest{})
	return err
}

func callFetch(t *testing.T, path string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := kadranv1.NewGitFetchServiceClient(dial(t, path)).FetchArchive(ctx, goodReq())
	if err != nil {
		return err
	}
	_, err = st.Recv()
	return err
}

func TestPeerAdminRejectsOtherUID(t *testing.T) {
	f := &fakeSvc{statusConn: &kadranv1.GitHubConnection{}}
	s, err := newAdminServer(f, uint32(os.Getuid())+1) // kendi uid'i listede DEĞİL
	if err != nil {
		t.Fatal(err)
	}

	if err := callStatus(t, serve(t, s)); err == nil {
		t.Fatal("admin socket accepted a caller outside the allow list")
	}
}

func TestPeerAdminAcceptsAllowedUID(t *testing.T) {
	f := &fakeSvc{statusConn: &kadranv1.GitHubConnection{Login: "octo"}}
	s, err := newAdminServer(f, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}

	if err := callStatus(t, serve(t, s)); err != nil {
		t.Fatalf("admin socket rejected the allowed uid: %v", err)
	}
}

func TestPeerFetchRejectsNonRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: the non-root half cannot be measured")
	}
	f := &fakeSvc{}
	s, err := newFetchServer(f)
	if err != nil {
		t.Fatal(err)
	}

	if err := callFetch(t, serve(t, s)); err == nil {
		t.Fatal("fetch socket accepted a non-root caller")
	}
	if f.decideCalls != 0 {
		t.Fatal("handler ran for a rejected caller")
	}
}

func TestPeerFetchAcceptsRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("root-only half: needs uid 0")
	}
	f := &fakeSvc{} // NotConnected: tek karar iletisi
	s, err := newFetchServer(f)
	if err != nil {
		t.Fatal(err)
	}

	if err := callFetch(t, serve(t, s)); err != nil {
		t.Fatalf("fetch socket rejected root: %v", err)
	}
}
