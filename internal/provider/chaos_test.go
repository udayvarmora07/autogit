package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type blockingRoundTripper struct{}

func (blockingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	<-request.Context().Done()
	return nil, request.Context().Err()
}

func TestRESTNetworkStallReturnsTypedTimeout(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	config := restTestConfig(server.URL)
	config.HTTPClient = &http.Client{Transport: blockingRoundTripper{}}
	provider, err := NewGitHubREST(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err = provider.GetRepository(ctx, "acme", "repo", "")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("network stall error=%v, want typed timeout", err)
	}
}

type partialResponseBody struct {
	data []byte
	done bool
}

func (b *partialResponseBody) Read(dst []byte) (int, error) {
	if b.done {
		return 0, errors.New("simulated connection reset")
	}
	b.done = true
	return copy(dst, b.data), nil
}

func (b *partialResponseBody) Close() error { return nil }

type fixedResponseRoundTripper struct {
	body io.ReadCloser
}

func (r fixedResponseRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: r.body}, nil
}

func TestRESTPartialResponseFailsClosedWithoutBodyLeak(t *testing.T) {
	config := restTestConfig("http://provider.invalid")
	config.HTTPClient = &http.Client{Transport: fixedResponseRoundTripper{body: &partialResponseBody{data: []byte(`{"full_name":"acme/repo"`)}}}
	provider, err := NewGitHubREST(config)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = provider.GetRepository(context.Background(), "acme", "repo", "")
	if !errors.Is(err, ErrOffline) || strings.Contains(err.Error(), "acme/repo") {
		t.Fatalf("partial response error=%v, want redacted offline failure", err)
	}
}

func TestFakePushReplayProducesOneExternalEffect(t *testing.T) {
	fake := NewFake()
	fake.Add("owner/repo", "private")
	sha := strings.Repeat("a", 40)
	if err := fake.Push(context.Background(), "owner/repo", sha, "main"); err != nil {
		t.Fatal(err)
	}
	if err := fake.Push(context.Background(), "owner/repo", sha, "main"); err != nil {
		t.Fatal(err)
	}
	calls := fake.Calls()
	if len(calls) != 1 || calls[0].Operation != "push" {
		t.Fatalf("replay calls=%+v, want one push", calls)
	}
}
