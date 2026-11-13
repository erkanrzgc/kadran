package main

import (
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
)

func TestAdminUIDRejectsRootAndSelf(t *testing.T) {
	if err := checkAdminUID(0, 1000); err == nil {
		t.Error("root admin accepted")
	}
	if err := checkAdminUID(1000, 1000); err == nil {
		t.Error("admin equal to the daemon user accepted")
	}
	if err := checkAdminUID(1001, 1000); err != nil {
		t.Errorf("valid admin rejected: %v", err)
	}
}

func TestServeBothStopsOtherWhenOneListenerCloses(t *testing.T) {
	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- serveBoth(t.Context(), grpc.NewServer(), ln1, grpc.NewServer(), ln2) }()
	time.Sleep(100 * time.Millisecond) // iki sunucunun Serve'e girmesine izin ver

	_ = ln1.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serveBoth did not return after one listener closed")
	}
	if c, err := net.DialTimeout("tcp", ln2.Addr().String(), time.Second); err == nil {
		_ = c.Close()
		t.Fatal("the other server is still accepting connections")
	}
}
