package api

import (
	"context"
	"google.golang.org/grpc/status"
)

func (s *Server) acquireApp(ctx context.Context, appID string) (func(), error) {
	release, err := s.coordinator.Acquire(ctx, appID)
	if err != nil {
		return nil, status.FromContextError(err).Err()
	}
	return release, nil
}
