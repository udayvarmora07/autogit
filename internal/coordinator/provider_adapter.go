package coordinator

import (
	"context"
	"errors"

	"autogit/internal/provider"
)

// PublicationProviderAdapter adapts the provider publication port to the
// coordinator's push port without creating or mutating remotes.
type PublicationProviderAdapter struct {
	Provider provider.PublicationProvider
}

func (a PublicationProviderAdapter) Push(ctx context.Context, r PushRequest) error {
	if a.Provider == nil {
		return errors.New("publication provider missing")
	}
	return a.Provider.Publish(ctx, publicationPushRequest(r))
}

func (a PublicationProviderAdapter) ConfirmPush(ctx context.Context, r PushRequest) (ConfirmPushOutcome, error) {
	if a.Provider == nil {
		return "", errors.New("publication provider missing")
	}
	outcome, err := a.Provider.ConfirmPush(ctx, publicationPushRequest(r))
	var mapped ConfirmPushOutcome
	switch outcome {
	case provider.PushMissing:
		mapped = PushMissing
	case provider.PushPresent:
		mapped = PushPresent
	case provider.PushConflict:
		mapped = PushConflict
	default:
		if err != nil {
			return "", err
		}
		return "", errors.New("invalid provider confirmation outcome")
	}
	return mapped, err
}

func publicationPushRequest(r PushRequest) provider.PushRequest {
	return provider.PushRequest{Owner: r.Owner, Name: r.Name, Ref: r.Ref, SHA: r.CommitSHA}
}

var _ Provider = PublicationProviderAdapter{}
