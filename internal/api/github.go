package api

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// Aşağıdaki dört yöntem GitHub cihaz akışının (K-167) taslaklarıdır.
// Görev 9 bunları gerçek uygulamayla değiştirir. Yöntemler yalnız
// yöneticiye açıktır: deployAllowed listesine EKLENMEZ.

// StartGitHubConnect, cihaz akışını başlatır (taslak).
func (s *Server) StartGitHubConnect(context.Context, *kadranv1.StartGitHubConnectRequest) (*kadranv1.StartGitHubConnectResponse, error) {
	return nil, status.Error(codes.Unimplemented, "github connect is not implemented yet")
}

// GetGitHubStatus, bağlantı durumunu döner (taslak).
func (s *Server) GetGitHubStatus(context.Context, *kadranv1.GetGitHubStatusRequest) (*kadranv1.GetGitHubStatusResponse, error) {
	return nil, status.Error(codes.Unimplemented, "github status is not implemented yet")
}

// DisconnectGitHub, bağlantıyı koparır (taslak).
func (s *Server) DisconnectGitHub(context.Context, *kadranv1.DisconnectGitHubRequest) (*kadranv1.DisconnectGitHubResponse, error) {
	return nil, status.Error(codes.Unimplemented, "github disconnect is not implemented yet")
}

// ResolveGitHubBranch, dalı commit karmasına çevirir (taslak).
func (s *Server) ResolveGitHubBranch(context.Context, *kadranv1.ResolveGitHubBranchRequest) (*kadranv1.ResolveGitHubBranchResponse, error) {
	return nil, status.Error(codes.Unimplemented, "github branch resolve is not implemented yet")
}
