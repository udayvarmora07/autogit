package provider

import (
	"context"
	"testing"
)

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
