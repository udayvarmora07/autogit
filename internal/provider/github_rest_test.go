package provider

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"autogit/internal/state"
)

func restTestConfig(serverURL string) GitHubRESTConfig {
	parsed, _ := url.Parse(serverURL)
	return GitHubRESTConfig{
		BaseURL:           serverURL + "/",
		Identity:          ProviderIdentity{Host: parsed.Host, Account: "alice", Owner: "acme"},
		Token:             "explicit-token",
		AllowInsecureHTTP: true,
		HTTPClient:        &http.Client{Timeout: 2 * time.Second},
	}
}

type recordingRESTPusher struct {
	called int
	owner  string
	name   string
	sha    string
	ref    string
	err    error
	onPush func()
}

func (p *recordingRESTPusher) Push(_ context.Context, remote, sha, ref string) error {
	p.called++
	parts := strings.Split(remote, "/")
	if len(parts) == 2 {
		p.owner, p.name = parts[0], parts[1]
	}
	p.sha, p.ref = sha, ref
	if p.onPush != nil {
		p.onPush()
	}
	return p.err
}

func TestGitHubRESTBindsIdentityAndHeadersBeforeCreation(t *testing.T) {
	t.Setenv("GH_TOKEN", "ambient-token")
	t.Setenv("GITHUB_TOKEN", "ambient-token")
	var seenUser, seenCreate atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer explicit-token" {
			t.Errorf("authorization=%q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != GitHubRESTAPIVersion {
			t.Errorf("api version=%q", got)
		}
		if GitHubRESTAPIVersion != "2026-03-10" {
			t.Errorf("current API version=%q", GitHubRESTAPIVersion)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("accept=%q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			seenUser.Add(1)
			_, _ = w.Write([]byte(`{"login":"alice"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/orgs/acme/repos":
			seenCreate.Add(1)
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if body["name"] != "repo" || body["private"] != true {
				t.Errorf("create body=%#v", body)
			}
			_, _ = w.Write([]byte(`{"full_name":"acme/repo","visibility":"private"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := NewGitHubREST(restTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := provider.Create(context.Background(), RemoteRequest{Owner: "acme", Name: "repo", Visibility: "private"})
	if err != nil || identity != "acme/repo" {
		t.Fatalf("identity=%q err=%v", identity, err)
	}
	if seenUser.Load() != 1 || seenCreate.Load() != 1 {
		t.Fatalf("user=%d create=%d", seenUser.Load(), seenCreate.Load())
	}
	if _, _, err := provider.GetRepository(context.Background(), "other", "repo", ""); !errors.Is(err, ErrInvalidProviderIdentity) {
		t.Fatalf("wrong owner error=%v", err)
	}
}

func TestGitHubRESTBoundsBodiesAndPreservesSafeRateMetadata(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		headers   map[string]string
		want      error
		wantRetry time.Duration
		wantID    string
	}{
		{name: "body limit", status: http.StatusOK, body: strings.Repeat("x", 100), want: ErrResponseLimit},
		{name: "rate limit", status: http.StatusTooManyRequests, body: `{"message":"token=must-not-leak"}`, headers: map[string]string{"Retry-After": "7", "X-GitHub-Request-Id": "req-123", "X-RateLimit-Remaining": "0"}, want: ErrRateLimit, wantRetry: 7 * time.Second, wantID: "req-123"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for key, value := range test.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			config := restTestConfig(server.URL)
			config.MaxResponseBytes = 32
			if test.name == "rate limit" {
				config.MaxResponseBytes = 256
			}
			provider, err := NewGitHubREST(config)
			if err != nil {
				t.Fatal(err)
			}
			_, meta, err := provider.GetRepository(context.Background(), "acme", "repo", "")
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
			if meta.RequestID != test.wantID || meta.RetryAfter != test.wantRetry {
				t.Fatalf("metadata=%+v", meta)
			}
			if err != nil && strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("response body leaked: %v", err)
			}
		})
	}
}

func TestGitHubRESTClassifiesForbiddenResponsesAndEscapesBranchRefs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/auth" {
			w.Header().Set("X-RateLimit-Remaining", "7")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"denied"}`))
			return
		}
		if r.URL.EscapedPath() != "/repos/acme/repo/git/ref/heads/feature%2Fx" {
			t.Errorf("escaped ref path=%q", r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"object":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`))
	}))
	defer server.Close()
	provider, err := NewGitHubREST(restTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := provider.GetRepository(context.Background(), "acme", "auth", ""); !errors.Is(err, ErrAuth) {
		t.Fatalf("forbidden auth error=%v", err)
	}
	sha, err := provider.Inspect(context.Background(), RemoteRequest{Owner: "acme", Name: "repo", Visibility: "private"}, "feature/x")
	if err != nil || sha != strings.Repeat("a", 40) {
		t.Fatalf("escaped ref sha=%q err=%v", sha, err)
	}
}

func TestGitHubRESTPublishesOnlyTheExactRefAndSHA(t *testing.T) {
	sha := strings.Repeat("b", 40)
	var pushed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"login":"alice"}`))
		case "/repos/acme/repo/git/ref/heads/main":
			if !pushed.Load() {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"object":{"sha":"` + sha + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	pusher := &recordingRESTPusher{}
	pusher.onPush = func() { pushed.Store(true) }
	config := restTestConfig(server.URL)
	config.Pusher = pusher
	provider, err := NewGitHubREST(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Publish(context.Background(), PushRequest{Owner: "acme", Name: "repo", Ref: "main", SHA: sha}); err != nil {
		t.Fatal(err)
	}
	if pusher.called != 1 || pusher.owner != "acme" || pusher.name != "repo" || pusher.sha != sha || pusher.ref != "main" {
		t.Fatalf("pusher=%+v", pusher)
	}
}

func TestGitHubRESTConditionalReadAndPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/repo":
			if r.Header.Get("If-None-Match") != "etag-1" {
				t.Errorf("If-None-Match=%q", r.Header.Get("If-None-Match"))
			}
			w.Header().Set("ETag", "etag-1")
			w.WriteHeader(http.StatusNotModified)
		case "/users/acme/repos":
			w.Header().Set("Link", `<http://example.test/users/acme/repos?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"full_name":"acme/one","visibility":"private"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider, err := NewGitHubREST(restTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, meta, err := provider.GetRepository(context.Background(), "acme", "repo", "etag-1")
	if !errors.Is(err, ErrNotModified) || meta.ETag != "etag-1" {
		t.Fatalf("conditional result meta=%+v err=%v", meta, err)
	}
	repositories, page, _, err := provider.ListRepositories(context.Background(), "acme", 1, 100)
	if err != nil || len(repositories) != 1 || !page.HasNext || page.NextPage != 2 {
		t.Fatalf("repositories=%+v page=%+v err=%v", repositories, page, err)
	}
}

func TestGitHubRESTRejectsMismatchedRepositoryIdentityAndFollowsBoundedPages(t *testing.T) {
	var pageCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/repo":
			_, _ = w.Write([]byte(`{"full_name":"other/repo","visibility":"private"}`))
		case "/users/acme/repos":
			pageCalls.Add(1)
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `<https://example.test/users/acme/repos?page=2>; rel="next"`)
				_, _ = w.Write([]byte(`[{"full_name":"acme/one","visibility":"private"}]`))
				return
			}
			_, _ = w.Write([]byte(`[{"full_name":"acme/two","visibility":"public"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider, err := NewGitHubREST(restTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := provider.GetRepository(context.Background(), "acme", "repo", ""); !errors.Is(err, ErrPostcondition) {
		t.Fatalf("mismatched repository error=%v", err)
	}
	repositories, _, err := provider.ListAllRepositories(context.Background(), "acme", 100)
	if err != nil || len(repositories) != 2 || repositories[1].FullName != "acme/two" || pageCalls.Load() != 2 {
		t.Fatalf("repositories=%+v calls=%d err=%v", repositories, pageCalls.Load(), err)
	}
}

func TestGitHubAppTokenSourceRefreshesInMemoryWithLeastPrivilege(t *testing.T) {
	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/42/access_tokens" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer eyJ") {
			t.Errorf("missing app JWT")
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != GitHubRESTAPIVersion {
			t.Errorf("api version=%q", got)
		}
		var body struct {
			RepositoryIDs []int64           `json:"repository_ids"`
			Permissions   map[string]string `json:"permissions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.RepositoryIDs) != 1 || body.RepositoryIDs[0] != 99 || body.Permissions["contents"] != "write" {
			t.Fatalf("least privilege body=%+v", body)
		}
		_, _ = w.Write([]byte(`{"token":"installation-token","expires_at":"2099-01-01T00:00:00Z"}`))
	}))
	defer server.Close()
	source, err := NewGitHubAppTokenSource(GitHubAppTokenConfig{BaseURL: server.URL + "/", AppID: 7, InstallationID: 42, PrivateKey: privateKey, RepositoryIDs: []int64{99}, Permissions: map[string]string{"contents": "write"}, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := source.Token(context.Background())
	if err != nil || first != "installation-token" {
		t.Fatalf("first token=%q err=%v", first, err)
	}
	second, err := source.Token(context.Background())
	if err != nil || second != first || calls.Load() != 1 {
		t.Fatalf("cached token=%q err=%v calls=%d", second, err, calls.Load())
	}
	source.Clear()
	if _, err := source.Token(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatalf("refresh err=%v calls=%d", err, calls.Load())
	}
}

func TestGitHubRESTRejectsAppTokenSourceForAnotherHost(t *testing.T) {
	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewGitHubAppTokenSource(GitHubAppTokenConfig{BaseURL: "https://tokens.example/", AppID: 7, InstallationID: 42, PrivateKey: privateKey, RepositoryIDs: []int64{99}, Permissions: map[string]string{"contents": "read"}})
	if err != nil {
		t.Fatal(err)
	}
	config := restTestConfig("https://api.example/")
	config.TokenSource = source
	config.Token = ""
	if _, err := NewGitHubREST(config); !errors.Is(err, ErrInvalidProviderIdentity) {
		t.Fatalf("mismatched token source error=%v", err)
	}
}

func TestGitHubRESTRejectsWrongConfiguredHostAndAppTokenSourceURL(t *testing.T) {
	config := restTestConfig("https://api.example/")
	config.Identity.Host = "other.example"
	if _, err := NewGitHubREST(config); !errors.Is(err, ErrInvalidProviderIdentity) {
		t.Fatalf("wrong configured host error=%v", err)
	}
	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewGitHubAppTokenSource(GitHubAppTokenConfig{BaseURL: "https://tokens.example/?redirect=unsafe", AppID: 7, InstallationID: 42, PrivateKey: privateKey, RepositoryIDs: []int64{99}, Permissions: map[string]string{"contents": "read"}})
	if err == nil {
		t.Fatal("App token source accepted a query-bearing base URL")
	}
}

func TestGitHubAppTokenSourceRefreshesBeforeExpiryAndRejectsUnscopedConfig(t *testing.T) {
	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"token":"ghs_stateless_token","expires_at":"2026-09-07T13:00:00Z"}`))
	}))
	defer server.Close()
	config := GitHubAppTokenConfig{BaseURL: server.URL + "/", AppID: 7, InstallationID: 42, PrivateKey: privateKey, RepositoryIDs: []int64{99}, Permissions: map[string]string{"checks": "write"}, AllowInsecureHTTP: true, Clock: func() time.Time { return now }}
	source, err := NewGitHubAppTokenSource(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(56 * time.Minute)
	if _, err := source.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("token refresh calls=%d, want 2", calls.Load())
	}
	for _, repositories := range [][]int64{nil, {99, 99}} {
		config.RepositoryIDs = repositories
		if _, err := NewGitHubAppTokenSource(config); err == nil {
			t.Fatalf("unacceptable repository scope %#v was accepted", repositories)
		}
	}
	config.RepositoryIDs = []int64{99}
	config.Permissions = nil
	if _, err := NewGitHubAppTokenSource(config); err == nil {
		t.Fatal("empty permission scope was accepted")
	}
}

func TestGitHubRESTEnterprisePolicyAndCheckRunProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"login":"alice"}`))
		case "/meta":
			w.Header().Set("X-GitHub-Enterprise-Version", "3.14.2")
			_, _ = w.Write([]byte(`{}`))
		case "/repos/acme/repo/check-runs":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(body)
			if strings.Contains(string(encoded), "token=secret") || strings.Contains(string(encoded), "/home/private") {
				t.Fatalf("check run leaked sensitive text: %s", encoded)
			}
			_, _ = w.Write([]byte(`{"id":123,"html_url":"https://github.com/acme/repo/check-runs/123","head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","external_id":"evidence-1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config := restTestConfig(server.URL)
	config.EnterprisePolicy = EnterpriseVersionPolicy{MinimumMajor: 3, MaximumMajor: 3, RequireVersion: true}
	provider, err := NewGitHubREST(config)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := provider.Negotiate(context.Background())
	if err != nil || !capability.Enterprise || capability.EnterpriseVersion != "3.14.2" {
		t.Fatalf("capability=%+v err=%v", capability, err)
	}
	sha := strings.Repeat("a", 40)
	result, err := provider.PublishCheckRun(context.Background(), CheckRunProjection{Owner: "acme", Repository: "repo", Name: "AutoGit verification", HeadSHA: sha, EvidenceID: "evidence-1", Status: "completed", Conclusion: "success", Title: "verification", Summary: "token=secret", Text: "/home/private"})
	if err != nil || result.ID != 123 || result.HeadSHA != sha {
		t.Fatalf("check run=%+v err=%v", result, err)
	}
}

func TestGitHubRESTEnterpriseVersionMatrixFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name       string
		version    string
		wantAccept bool
	}{
		{name: "minimum supported", version: "3.0.0", wantAccept: true},
		{name: "current major", version: "3.19.0", wantAccept: true},
		{name: "future policy major", version: "4.1.0", wantAccept: true},
		{name: "old major", version: "2.99.0"},
		{name: "malformed", version: "three.nineteen.0"},
		{name: "missing minor", version: "3"},
		{name: "missing metadata", version: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.version != "" {
					w.Header().Set("X-GitHub-Enterprise-Version", test.version)
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			config := restTestConfig(server.URL)
			config.EnterprisePolicy = DefaultEnterpriseVersionPolicy
			provider, err := NewGitHubREST(config)
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Negotiate(context.Background())
			if (err == nil) != test.wantAccept {
				t.Fatalf("version=%q accepted=%v err=%v", test.version, err == nil, err)
			}
		})
	}
}

func TestGitHubRESTRejectsAmbientCredentialAndUnsupportedEnterprise(t *testing.T) {
	t.Setenv("GH_TOKEN", "ambient-token")
	t.Setenv("GITHUB_TOKEN", "ambient-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("ambient token must not cause a request")
	}))
	defer server.Close()
	config := restTestConfig(server.URL)
	config.Token = ""
	provider, err := NewGitHubREST(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := provider.GetRepository(context.Background(), "acme", "repo", ""); !errors.Is(err, ErrAuth) {
		t.Fatalf("ambient credential error=%v", err)
	}

	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer metaServer.Close()
	metaConfig := restTestConfig(metaServer.URL)
	metaConfig.EnterprisePolicy = EnterpriseVersionPolicy{MinimumMajor: 3, MaximumMajor: 4, RequireVersion: true}
	enterprise, err := NewGitHubREST(metaConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enterprise.Negotiate(context.Background()); !errors.Is(err, ErrUnsupportedServer) {
		t.Fatalf("missing enterprise version error=%v", err)
	}
}

func TestGitHubRESTRejectsAuthenticatedAccountMismatchBeforeMutation(t *testing.T) {
	var createCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			_, _ = w.Write([]byte(`{"login":"different-account"}`))
			return
		}
		if r.URL.Path == "/orgs/acme/repos" {
			createCalls.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	provider, err := NewGitHubREST(restTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Create(context.Background(), RemoteRequest{Owner: "acme", Name: "repo", Visibility: "private"})
	if !errors.Is(err, ErrAuth) || createCalls.Load() != 0 {
		t.Fatalf("mismatch error=%v create calls=%d", err, createCalls.Load())
	}
}

func TestGitHubAppTokenSourceMapsRevocationToAuthWithoutLeakingResponse(t *testing.T) {
	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", "revoked-1")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"installation token revoked secret=must-not-leak"}`))
	}))
	defer server.Close()
	source, err := NewGitHubAppTokenSource(GitHubAppTokenConfig{BaseURL: server.URL + "/", AppID: 7, InstallationID: 42, PrivateKey: privateKey, RepositoryIDs: []int64{99}, Permissions: map[string]string{"contents": "write"}, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	err = func() error {
		_, tokenErr := source.Token(context.Background())
		return tokenErr
	}()
	var restErr *RESTError
	if !errors.As(err, &restErr) || !errors.Is(err, ErrAuth) || restErr.RequestID != "revoked-1" || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("revocation error=%v request_id=%q", err, restErr.RequestID)
	}
}

func TestGitHubRESTReconcilesDurableRepositoryIntentWithoutDuplicateCreate(t *testing.T) {
	var created atomic.Bool
	var createCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"login":"alice"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo":
			if !created.Load() {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"full_name":"acme/repo","name":"repo","visibility":"private"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/orgs/acme/repos":
			createCalls.Add(1)
			created.Store(true)
			_, _ = w.Write([]byte(`{"full_name":"acme/repo","name":"repo","visibility":"private"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config := restTestConfig(server.URL)
	config.Identity.Account = "alice"
	config.Identity.Owner = "acme"
	hosted, err := NewGitHubREST(config)
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	binder := &transactionBinder{addErr: errors.New("local attach response lost")}
	request := RemoteCreateRequest{ID: "rest-reconcile", RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "acme", Name: "repo", Visibility: "private"}
	tx := RepositoryTransaction{State: db, Hosted: hosted, Git: binder}
	if _, err := tx.Create(context.Background(), request); err == nil {
		t.Fatal("first attachment unexpectedly succeeded")
	}
	job, err := db.RemoteJob(request.ID)
	if err != nil || job.State != state.RemoteCreated {
		t.Fatalf("durable job=%+v err=%v", job, err)
	}
	binder.addErr = nil
	if got, err := tx.Create(context.Background(), request); err != nil || got != "acme/repo" {
		t.Fatalf("reconciled identity=%q err=%v", got, err)
	}
	if createCalls.Load() != 1 || binder.add != 2 {
		t.Fatalf("create calls=%d local adds=%d, want one create and two attach attempts", createCalls.Load(), binder.add)
	}
	job, err = db.RemoteJob(request.ID)
	if err != nil || job.State != state.RemoteAttached {
		t.Fatalf("attached job=%+v err=%v", job, err)
	}
}
