package provider

import (
	"context"
	"testing"
)

func FuzzRemoteIdentityValidationNeverPanics(f *testing.F) {
	f.Add("owner", "repo", "private")
	f.Add("../owner", "repo", "public")
	f.Add("owner", "repo.lock", "unknown")
	f.Fuzz(func(t *testing.T, owner, name, visibility string) {
		_ = ValidateRemoteRequest(RemoteRequest{Owner: owner, Name: name, Visibility: visibility})
	})
}

func FuzzProviderRefValidationNeverPanics(f *testing.F) {
	f.Add("owner", "repo", "refs/heads/main")
	f.Add("owner", "repo", "-danger")
	f.Fuzz(func(t *testing.T, owner, name, ref string) {
		_, _ = (&Fake{}).InspectRef(context.TODO(), owner+"/"+name, ref)
	})
}
