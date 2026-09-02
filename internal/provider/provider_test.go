package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIsRetryableClassifiesTypedProviderErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"offline", ErrOffline, true},
		{"timeout", ErrTimeout, true},
		{"rate limit", ErrRateLimit, true},
		{"auth", ErrAuth, false},
		{"non fast forward", ErrNonFastForward, false},
		{"protected branch", ErrProtectedBranch, false},
		{"secret scanning", ErrSecretScanning, false},
		{"collision", ErrCollision, false},
		{"ref conflict", ErrRefConflict, false},
		{"postcondition", ErrPostcondition, false},
		{"output limit", ErrOutputLimit, false},
		{"local only", ErrLocalOnly, false},
		{"unsupported push", ErrUnsupportedPush, false},
		{"remote binding", ErrRemoteBinding, false},
		{"unknown", errors.New("unknown provider failure"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryable(fmt.Errorf("wrapped: %w", tc.err)); got != tc.want {
				t.Fatalf("IsRetryable(%v)=%v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestFakeProviderRecordsExactDestinationAndRefPostcondition(t *testing.T) {
	f := NewFake()
	remote, err := f.EnsureRemote(context.Background(), "owner", "name", "private")
	if err != nil {
		t.Fatal(err)
	}
	if remote != "owner/name" {
		t.Fatalf("remote=%q", remote)
	}
	sha := "0123456789abcdef0123456789abcdef01234567"
	if err := f.Push(context.Background(), remote, sha, "feature/x"); err != nil {
		t.Fatal(err)
	}
	if got := f.Ref("feature/x"); got != sha {
		t.Fatalf("ref=%q", got)
	}
	if len(f.Calls()) != 2 {
		t.Fatalf("calls=%d", len(f.Calls()))
	}
}

func TestFakeProviderDoesNotAttachCollision(t *testing.T) {
	f := NewFake()
	f.Add("owner/name", "private")
	if _, err := f.EnsureRemote(context.Background(), "owner", "name", "private"); err == nil {
		t.Fatal("collision accepted")
	}
}

func TestFakeProviderValidatesRemoteRefBeforeInspection(t *testing.T) {
	f := NewFake()
	for name, remote := range map[string]string{"missing-owner": "repo", "extra-part": "owner/repo/extra", "bad-owner": "owner! /repo"} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.InspectRef(context.Background(), remote, "refs/heads/main"); err == nil || !strings.Contains(err.Error(), "invalid ref inspection") {
				t.Fatal("invalid remote accepted")
			}
		})
	}
	if _, err := f.InspectRef(context.Background(), "owner/repo", "bad ref"); err == nil || !strings.Contains(err.Error(), "invalid ref inspection") {
		t.Fatal("invalid ref accepted")
	}
}
