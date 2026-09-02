// Package session owns the boundary between a session lifecycle event and a
// repository baseline. It keeps source bytes in memory and persists only the
// bounded identity/digest evidence required for replay.
package session

import (
	"context"
	"errors"

	"autogit/internal/repository"
)

type Request struct {
	SessionID    string
	RepositoryID string
	ClientID     string
	Root         string
}

type Store interface {
	RecordSessionBaseline(context.Context, string, string, string, repository.Baseline) error
}

type CaptureFunc func(context.Context, repository.Runner, string) (repository.Baseline, error)

type Service struct {
	Runner  repository.Runner
	Store   Store
	Capture CaptureFunc
}

func (s Service) CaptureAndRecord(ctx context.Context, req Request) (repository.Baseline, error) {
	if req.SessionID == "" || req.RepositoryID == "" || req.ClientID == "" || req.Root == "" {
		return repository.Baseline{}, errors.New("session baseline request is incomplete")
	}
	if s.Runner == nil || s.Store == nil {
		return repository.Baseline{}, errors.New("session baseline dependencies are required")
	}
	capture := s.Capture
	if capture == nil {
		capture = repository.CaptureBaseline
	}
	baseline, err := capture(ctx, s.Runner, req.Root)
	if err != nil {
		return repository.Baseline{}, err
	}
	if err := s.Store.RecordSessionBaseline(ctx, req.SessionID, req.RepositoryID, req.ClientID, baseline); err != nil {
		return repository.Baseline{}, err
	}
	return baseline, nil
}
