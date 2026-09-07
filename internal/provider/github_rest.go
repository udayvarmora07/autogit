package provider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"autogit/internal/security"
)

const (
	GitHubRESTAPIVersion = "2022-11-28"
	defaultRESTBodyLimit = int64(1 << 20)
	defaultRESTUserAgent = "autogit-provider/1"
)

var (
	ErrNotModified             = errors.New("provider response not modified")
	ErrInvalidProviderIdentity = errors.New("invalid provider identity")
	ErrResponseLimit           = errors.New("provider response exceeded limit")
	ErrUnsupportedServer       = errors.New("unsupported GitHub server")
)

// TokenSource returns a short-lived provider credential. Implementations must
// keep credentials in memory; the REST transport never reads ambient token
// environment variables.
type TokenSource interface {
	Token(context.Context) (string, error)
}

type staticTokenSource string

func (s staticTokenSource) Token(context.Context) (string, error) {
	if strings.TrimSpace(string(s)) == "" || strings.ContainsAny(string(s), "\r\n") {
		return "", ErrAuth
	}
	return string(s), nil
}

// StaticToken returns an explicit in-memory token source. It is intentionally
// separate from environment loading so GH_TOKEN/GITHUB_TOKEN cannot silently
// change the provider target.
func StaticToken(token string) TokenSource { return staticTokenSource(token) }

// ProviderIdentity binds every REST operation to one host, authenticated
// account, and requested owner. The binding is checked before mutations.
type ProviderIdentity struct {
	Host    string
	Account string
	Owner   string
}

func (i ProviderIdentity) validate() error {
	if !validHost(i.Host) || !validSimpleIdentity(i.Account) || !validSimpleIdentity(i.Owner) {
		return ErrInvalidProviderIdentity
	}
	return nil
}

// EnterpriseVersionPolicy is the deliberately small support window for
// GitHub Enterprise Server. Unknown versions are rejected when RequireVersion
// is true; public github.com does not require an Enterprise version header.
type EnterpriseVersionPolicy struct {
	MinimumMajor   int
	MaximumMajor   int
	RequireVersion bool
}

var DefaultEnterpriseVersionPolicy = EnterpriseVersionPolicy{
	MinimumMajor:   3,
	MaximumMajor:   4,
	RequireVersion: true,
}

// GitHubRESTConfig configures the typed GitHub REST boundary. BaseURL is the
// API root, such as https://api.github.com/ or https://ghe.example/api/v3/.
// HTTP is accepted only when AllowInsecureHTTP is explicitly enabled, which
// is intended for loopback contract tests.
type GitHubRESTConfig struct {
	BaseURL           string
	APIVersion        string
	Identity          ProviderIdentity
	Token             string
	TokenSource       TokenSource
	HTTPClient        *http.Client
	MaxResponseBytes  int64
	UserAgent         string
	EnterprisePolicy  EnterpriseVersionPolicy
	AllowInsecureHTTP bool
	Pusher            Pusher
}

// GitHubREST is a typed, bounded REST provider. It implements repository
// creation/inspection and publication orchestration while Git pushes remain
// delegated to the exact local Git pusher.
type GitHubREST struct {
	baseURL          *url.URL
	apiVersion       string
	identity         ProviderIdentity
	tokens           TokenSource
	client           *http.Client
	maxResponseBytes int64
	userAgent        string
	enterprisePolicy EnterpriseVersionPolicy
	pusher           Pusher
}

var _ SafeProvider = (*GitHubREST)(nil)
var _ HostedRepository = (*GitHubREST)(nil)
var _ PublicationProvider = (*GitHubREST)(nil)

// NewGitHubREST creates a REST provider with an explicit identity and
// credential. It does not inspect GH_TOKEN, GITHUB_TOKEN, gh config, or the
// process environment.
func NewGitHubREST(config GitHubRESTConfig) (*GitHubREST, error) {
	if config.BaseURL == "" {
		return nil, errors.New("GitHub REST base URL is required")
	}
	base, err := url.Parse(config.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil {
		return nil, errors.New("invalid GitHub REST base URL")
	}
	if base.Scheme != "https" && !config.AllowInsecureHTTP {
		return nil, errors.New("GitHub REST requires HTTPS")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/"
	host := base.Host
	if config.Identity.Host == "" {
		config.Identity.Host = host
	}
	if !strings.EqualFold(config.Identity.Host, host) {
		return nil, ErrInvalidProviderIdentity
	}
	if err := config.Identity.validate(); err != nil {
		return nil, err
	}
	apiVersion := config.APIVersion
	if apiVersion == "" {
		apiVersion = GitHubRESTAPIVersion
	}
	if !validAPIVersion(apiVersion) {
		return nil, errors.New("invalid GitHub REST API version")
	}
	var tokens TokenSource
	if config.TokenSource != nil && config.Token != "" {
		return nil, errors.New("configure either Token or TokenSource, not both")
	}
	if config.TokenSource != nil {
		tokens = config.TokenSource
	} else {
		tokens = StaticToken(config.Token)
	}
	if config.MaxResponseBytes <= 0 {
		config.MaxResponseBytes = defaultRESTBodyLimit
	}
	if config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("GitHub REST response limit is too large")
	}
	if config.UserAgent == "" {
		config.UserAgent = defaultRESTUserAgent
	}
	policy := config.EnterprisePolicy
	if policy.MinimumMajor == 0 && policy.MaximumMajor == 0 && !policy.RequireVersion {
		policy = DefaultEnterpriseVersionPolicy
	}
	return &GitHubREST{
		baseURL: base, apiVersion: apiVersion, identity: config.Identity,
		tokens: tokens, client: chooseHTTPClient(config.HTTPClient),
		maxResponseBytes: config.MaxResponseBytes, userAgent: config.UserAgent,
		enterprisePolicy: policy, pusher: config.Pusher,
	}, nil
}

func chooseHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func validHost(host string) bool {
	if host == "" || len(host) > 255 || strings.ContainsAny(host, "\r\n/@") {
		return false
	}
	for _, r := range host {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func validSimpleIdentity(value string) bool {
	return value != "" && len(value) <= 100 && valid(value)
}

func validAPIVersion(version string) bool {
	if len(version) != 10 || version[4] != '-' || version[7] != '-' {
		return false
	}
	_, errA := strconv.Atoi(version[:4])
	_, errB := strconv.Atoi(version[5:7])
	_, errC := strconv.Atoi(version[8:])
	return errA == nil && errB == nil && errC == nil
}

// RESTResponse records safe transport metadata. Response bodies are never
// retained here because provider errors may contain credentials or source.
type RESTResponse struct {
	StatusCode        int
	RequestID         string
	ETag              string
	RetryAfter        time.Duration
	RateLimitRemain   int64
	RateLimitReset    time.Time
	EnterpriseVersion string
	Next              string
	Previous          string
}

// RESTError is a redacted, typed HTTP failure. RequestID is safe diagnostic
// metadata supplied by GitHub; response bodies are intentionally omitted.
type RESTError struct {
	StatusCode      int
	RequestID       string
	RetryAfter      time.Duration
	RateLimitRemain int64
	RateLimitReset  time.Time
}

func (e *RESTError) Error() string {
	return fmt.Sprintf("GitHub REST request failed with status %d", e.StatusCode)
}

func (e *RESTError) Is(target error) bool {
	switch {
	case target == ErrAuth:
		return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden && e.RateLimitRemain != 0
	case target == ErrRateLimit:
		return e.StatusCode == http.StatusTooManyRequests || e.StatusCode == http.StatusForbidden && e.RateLimitRemain == 0
	case target == ErrOffline:
		return e.StatusCode >= 500
	case target == ErrCollision:
		return e.StatusCode == http.StatusConflict
	case target == ErrRefAbsent:
		return e.StatusCode == http.StatusNotFound
	}
	return false
}

type repositoryResponse struct {
	FullName   string `json:"full_name"`
	Visibility string `json:"visibility"`
	Name       string `json:"name"`
	Owner      struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// Repository is a bounded repository identity returned by a list/read call.
type Repository struct {
	FullName   string
	Visibility string
}

// PageInfo describes GitHub's RFC 5988 pagination links without exposing raw
// response headers or URLs to durable state.
type PageInfo struct {
	NextPage     int
	PreviousPage int
	HasNext      bool
	HasPrevious  bool
}

// GetRepository performs a conditional, identity-bound repository read.
func (g *GitHubREST) GetRepository(ctx context.Context, owner, name, etag string) (Repository, RESTResponse, error) {
	if err := g.validateOwner(owner); err != nil || !validSimpleIdentity(name) {
		return Repository{}, RESTResponse{}, ErrInvalidProviderIdentity
	}
	var payload repositoryResponse
	meta, err := g.do(ctx, http.MethodGet, "repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name), etag, nil, &payload)
	if err != nil {
		return Repository{}, meta, err
	}
	return Repository{FullName: payload.FullName, Visibility: payload.Visibility}, meta, nil
}

// ListRepositories lists one explicitly bound owner's repositories and
// follows GitHub pagination through page/per_page query parameters.
func (g *GitHubREST) ListRepositories(ctx context.Context, owner string, page, perPage int) ([]Repository, PageInfo, RESTResponse, error) {
	if err := g.validateOwner(owner); err != nil {
		return nil, PageInfo{}, RESTResponse{}, err
	}
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 || perPage > 100 {
		perPage = 100
	}
	endpoint := "users/" + url.PathEscape(owner) + "/repos?page=" + strconv.Itoa(page) + "&per_page=" + strconv.Itoa(perPage)
	var payload []repositoryResponse
	meta, err := g.do(ctx, http.MethodGet, endpoint, "", nil, &payload)
	if err != nil {
		return nil, PageInfo{}, meta, err
	}
	repositories := make([]Repository, 0, len(payload))
	for _, item := range payload {
		repositories = append(repositories, Repository{FullName: item.FullName, Visibility: item.Visibility})
	}
	return repositories, parsePageInfo(meta.Next, meta.Previous), meta, nil
}

func parsePageInfo(next, previous string) PageInfo {
	return PageInfo{NextPage: linkPage(next), PreviousPage: linkPage(previous), HasNext: next != "", HasPrevious: previous != ""}
}

func linkPage(link string) int {
	if link == "" {
		return 0
	}
	u, err := url.Parse(link)
	if err != nil {
		return 0
	}
	page, _ := strconv.Atoi(u.Query().Get("page"))
	return page
}

func (g *GitHubREST) validateOwner(owner string) error {
	if err := g.identity.validate(); err != nil || owner != g.identity.Owner {
		return ErrInvalidProviderIdentity
	}
	return nil
}

func (g *GitHubREST) validateRequest(request RemoteRequest) error {
	if err := validIdentity(request); err != nil || request.Owner != g.identity.Owner {
		return ErrInvalidProviderIdentity
	}
	return nil
}

func (g *GitHubREST) verifyAccount(ctx context.Context) error {
	var payload struct {
		Login string `json:"login"`
	}
	_, err := g.do(ctx, http.MethodGet, "user", "", nil, &payload)
	if err != nil {
		return err
	}
	if payload.Login != g.identity.Account {
		return &ProviderError{Kind: KindAuth, Err: ErrAuth}
	}
	return nil
}

// Create creates exactly the configured owner's private or public repository,
// then verifies the returned full name and visibility before reporting success.
func (g *GitHubREST) Create(ctx context.Context, request RemoteRequest) (string, error) {
	if err := g.validateRequest(request); err != nil {
		return "", err
	}
	if err := g.verifyAccount(ctx); err != nil {
		return "", err
	}
	endpoint := "orgs/" + url.PathEscape(request.Owner) + "/repos"
	if request.Owner == g.identity.Account {
		endpoint = "user/repos"
	}
	payload := map[string]any{"name": request.Name, "private": request.Visibility == "private"}
	var created repositoryResponse
	if _, err := g.do(ctx, http.MethodPost, endpoint, "", payload, &created); err != nil {
		return "", err
	}
	if created.FullName != request.Owner+"/"+request.Name || created.Visibility != request.Visibility {
		return "", &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
	}
	return created.FullName, nil
}

// ConfirmRepository verifies owner, name, and visibility without mutation.
func (g *GitHubREST) ConfirmRepository(ctx context.Context, request RemoteRequest) error {
	if err := g.validateRequest(request); err != nil {
		return err
	}
	if err := g.verifyAccount(ctx); err != nil {
		return err
	}
	repository, _, err := g.GetRepository(ctx, request.Owner, request.Name, "")
	if err != nil {
		return err
	}
	if repository.FullName != request.Owner+"/"+request.Name || repository.Visibility != request.Visibility {
		return &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
	}
	return nil
}

// Inspect reads one exact branch ref and returns its canonical object SHA.
func (g *GitHubREST) Inspect(ctx context.Context, request RemoteRequest, ref string) (string, error) {
	if err := g.validateRequest(request); err != nil || !validRef(ref) {
		return "", ErrInvalidProviderIdentity
	}
	var payload struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	_, err := g.do(ctx, http.MethodGet, "repos/"+url.PathEscape(request.Owner)+"/"+url.PathEscape(request.Name)+"/git/ref/heads/"+url.PathEscape(ref), "", nil, &payload)
	if err != nil {
		return "", err
	}
	if !validSHA(payload.Object.SHA) {
		return "", &ProviderError{Kind: KindPostcondition, Err: ErrPostcondition}
	}
	return payload.Object.SHA, nil
}

// Publish performs exact-ref preflight, delegates the actual Git push, and
// confirms the same head SHA. The Check Run API is intentionally separate.
func (g *GitHubREST) Publish(ctx context.Context, request PushRequest) error {
	if g.pusher == nil || validIdentity(RemoteRequest{Owner: request.Owner, Name: request.Name, Visibility: "private"}) != nil || !validRef(request.Ref) || !validSHA(request.SHA) {
		if g.pusher == nil {
			return ErrUnsupportedPush
		}
		return errors.New("invalid push intent")
	}
	if err := g.verifyAccount(ctx); err != nil {
		return err
	}
	remote := request.Owner + "/" + request.Name
	current, err := g.Inspect(ctx, RemoteRequest{Owner: request.Owner, Name: request.Name, Visibility: "private"}, request.Ref)
	if err == nil && current != request.SHA {
		return ErrRefConflict
	}
	if err != nil && !errors.Is(err, ErrRefAbsent) {
		return err
	}
	if err := g.pusher.Push(ctx, remote, request.SHA, request.Ref); err != nil {
		return err
	}
	confirmed, err := g.Inspect(ctx, RemoteRequest{Owner: request.Owner, Name: request.Name, Visibility: "private"}, request.Ref)
	if err != nil {
		return err
	}
	if confirmed != request.SHA {
		return ErrPostcondition
	}
	return nil
}

// ConfirmPush returns an exact, non-mutating ref outcome.
func (g *GitHubREST) ConfirmPush(ctx context.Context, request PushRequest) (PushOutcome, error) {
	if request.Owner != g.identity.Owner || !validRef(request.Ref) || !validSHA(request.SHA) {
		return "", errors.New("invalid push intent")
	}
	actual, err := g.Inspect(ctx, RemoteRequest{Owner: request.Owner, Name: request.Name, Visibility: "private"}, request.Ref)
	if errors.Is(err, ErrRefAbsent) {
		return PushMissing, nil
	}
	if err != nil {
		return "", err
	}
	if actual != request.SHA {
		return PushConflict, nil
	}
	return PushPresent, nil
}

// HostCapability is the result of explicit public/Enterprise server
// negotiation. It is a capability fact, not a marketing-version claim.
type HostCapability struct {
	Host              string
	Enterprise        bool
	EnterpriseVersion string
	RESTAPIVersion    string
}

// Negotiate probes the server metadata and enforces the configured Enterprise
// version window. Public github.com is accepted without an Enterprise version.
func (g *GitHubREST) Negotiate(ctx context.Context) (HostCapability, error) {
	var payload struct {
		InstalledVersion string `json:"installed_version"`
	}
	meta, err := g.do(ctx, http.MethodGet, "meta", "", nil, &payload)
	if err != nil {
		return HostCapability{}, err
	}
	version := meta.EnterpriseVersion
	if version == "" {
		version = payload.InstalledVersion
	}
	public := strings.EqualFold(g.identity.Host, "api.github.com") || strings.EqualFold(g.identity.Host, "github.com")
	if public {
		return HostCapability{Host: g.identity.Host, RESTAPIVersion: g.apiVersion}, nil
	}
	if version == "" && g.enterprisePolicy.RequireVersion {
		return HostCapability{}, ErrUnsupportedServer
	}
	if version != "" {
		major, err := enterpriseMajor(version)
		if err != nil || major < g.enterprisePolicy.MinimumMajor || major > g.enterprisePolicy.MaximumMajor {
			return HostCapability{}, ErrUnsupportedServer
		}
	}
	return HostCapability{Host: g.identity.Host, Enterprise: true, EnterpriseVersion: version, RESTAPIVersion: g.apiVersion}, nil
}

func enterpriseMajor(version string) (int, error) {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0, ErrUnsupportedServer
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major <= 0 {
		return 0, ErrUnsupportedServer
	}
	return major, nil
}

// CheckAnnotation is deliberately metadata-only. It cannot carry source
// excerpts or secret values.
type CheckAnnotation struct {
	Path      string
	StartLine int
	EndLine   int
	Level     string
	Message   string
	Title     string
}

// CheckRunProjection is an optional projection of bounded local evidence. It
// is never the AutoGit source of truth and must name the exact candidate head.
type CheckRunProjection struct {
	Owner       string
	Repository  string
	Name        string
	HeadSHA     string
	EvidenceID  string
	Status      string
	Conclusion  string
	Title       string
	Summary     string
	Text        string
	DetailsURL  string
	Annotations []CheckAnnotation
}

// CheckRunResult is the bounded identity returned after a Check Run create.
type CheckRunResult struct {
	ID        int64
	URL       string
	HeadSHA   string
	RequestID string
}

// PublishCheckRun projects redacted, bounded evidence tied to one exact head
// SHA. It never updates durable AutoGit state or treats GitHub as authority.
func (g *GitHubREST) PublishCheckRun(ctx context.Context, projection CheckRunProjection) (CheckRunResult, error) {
	if projection.Owner != g.identity.Owner || !validSimpleIdentity(projection.Repository) || !validSHA(projection.HeadSHA) || !validEvidenceID(projection.EvidenceID) {
		return CheckRunResult{}, errors.New("invalid check run identity")
	}
	if projection.Name == "" || len(projection.Name) > 100 || !validCheckStatus(projection.Status, projection.Conclusion) {
		return CheckRunResult{}, errors.New("invalid check run status")
	}
	if projection.DetailsURL != "" {
		details, err := url.Parse(projection.DetailsURL)
		if err != nil || details.Scheme != "https" || details.User != nil || details.Host == "" || details.RawQuery != "" || details.Fragment != "" {
			return CheckRunResult{}, errors.New("invalid check run details URL")
		}
	}
	if len(projection.Annotations) > 50 {
		return CheckRunResult{}, errors.New("too many check run annotations")
	}
	if err := g.verifyAccount(ctx); err != nil {
		return CheckRunResult{}, err
	}
	annotations := make([]map[string]any, 0, len(projection.Annotations))
	for _, annotation := range projection.Annotations {
		if err := validateAnnotation(annotation); err != nil {
			return CheckRunResult{}, err
		}
		annotations = append(annotations, map[string]any{
			"path": annotation.Path, "start_line": annotation.StartLine, "end_line": annotation.EndLine,
			"annotation_level": annotation.Level, "message": boundedRedacted(annotation.Message, 512), "title": boundedRedacted(annotation.Title, 100),
		})
	}
	payload := map[string]any{
		"name": projection.Name, "head_sha": projection.HeadSHA, "status": projection.Status,
		"conclusion": projection.Conclusion, "external_id": projection.EvidenceID,
		"details_url": projection.DetailsURL,
		"output": map[string]any{
			"title": projection.Title, "summary": boundedRedacted(projection.Summary, 4096),
			"text": boundedRedacted(projection.Text, 4096), "annotations": annotations,
		},
	}
	var response struct {
		ID      int64  `json:"id"`
		URL     string `json:"html_url"`
		HeadSHA string `json:"head_sha"`
	}
	meta, err := g.do(ctx, http.MethodPost, "repos/"+url.PathEscape(projection.Owner)+"/"+url.PathEscape(projection.Repository)+"/check-runs", "", payload, &response)
	if err != nil {
		return CheckRunResult{}, err
	}
	if response.ID <= 0 || response.HeadSHA != projection.HeadSHA {
		return CheckRunResult{}, ErrPostcondition
	}
	return CheckRunResult{ID: response.ID, URL: response.URL, HeadSHA: response.HeadSHA, RequestID: meta.RequestID}, nil
}

func validEvidenceID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._:-", r)) {
			return false
		}
	}
	return true
}

func validCheckStatus(status, conclusion string) bool {
	switch status {
	case "queued", "in_progress":
		return conclusion == ""
	case "completed":
		switch conclusion {
		case "action_required", "cancelled", "failure", "neutral", "success", "skipped", "stale", "timed_out":
			return true
		}
	}
	return false
}

func validateAnnotation(annotation CheckAnnotation) error {
	if annotation.Path == "" || len(annotation.Path) > 512 || path.IsAbs(annotation.Path) || path.Clean(annotation.Path) != annotation.Path || strings.HasPrefix(annotation.Path, "../") || annotation.Path == ".." || annotation.StartLine <= 0 || annotation.EndLine < annotation.StartLine || annotation.EndLine > 1<<20 {
		return errors.New("invalid check run annotation path or line")
	}
	if annotation.Level != "failure" && annotation.Level != "warning" && annotation.Level != "notice" {
		return errors.New("invalid check run annotation level")
	}
	if annotation.Message == "" || len(annotation.Message) > 512 || annotation.Title == "" || len(annotation.Title) > 100 || strings.ContainsAny(annotation.Message+annotation.Title, "\r\n") {
		return errors.New("invalid check run annotation text")
	}
	return nil
}

func boundedRedacted(value string, max int) string {
	value = security.Redact(value)
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return ' '
		}
		return r
	}, value)
	if len(value) > max {
		return value[:max]
	}
	return value
}

func (g *GitHubREST) do(ctx context.Context, method, endpoint, etag string, input, output any) (RESTResponse, error) {
	if ctx == nil {
		return RESTResponse{}, errors.New("provider context is required")
	}
	if endpoint == "" || strings.HasPrefix(endpoint, "/") || strings.Contains(endpoint, "..") {
		return RESTResponse{}, errors.New("invalid provider endpoint")
	}
	relative, err := url.Parse(endpoint)
	if err != nil || relative.IsAbs() || relative.Host != "" {
		return RESTResponse{}, errors.New("invalid provider endpoint")
	}
	requestURL := *g.baseURL
	requestURL.Path = path.Join(g.baseURL.Path, relative.Path)
	requestURL.RawQuery = relative.RawQuery
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return RESTResponse{}, fmt.Errorf("encode provider request: %w", err)
		}
		body = strings.NewReader(string(encoded))
	}
	token, err := g.tokens.Token(ctx)
	if err != nil {
		return RESTResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return RESTResponse{}, fmt.Errorf("create provider request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", g.apiVersion)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", g.userAgent)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	response, err := g.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return RESTResponse{}, &ProviderError{Kind: KindTimeout, Err: ErrTimeout}
		}
		return RESTResponse{}, &ProviderError{Kind: KindOffline, Err: ErrOffline}
	}
	defer response.Body.Close()
	meta := responseMetadata(response)
	data, readErr := io.ReadAll(io.LimitReader(response.Body, g.maxResponseBytes+1))
	if readErr != nil {
		return meta, &ProviderError{Kind: KindOffline, Err: ErrOffline}
	}
	if int64(len(data)) > g.maxResponseBytes {
		return meta, ErrResponseLimit
	}
	if response.StatusCode == http.StatusNotModified {
		return meta, ErrNotModified
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return meta, &RESTError{StatusCode: response.StatusCode, RequestID: meta.RequestID, RetryAfter: meta.RetryAfter, RateLimitRemain: meta.RateLimitRemain, RateLimitReset: meta.RateLimitReset}
	}
	if output != nil && len(data) != 0 {
		if err := json.Unmarshal(data, output); err != nil {
			return meta, errors.New("invalid provider response")
		}
	}
	return meta, nil
}

func responseMetadata(response *http.Response) RESTResponse {
	meta := RESTResponse{StatusCode: response.StatusCode, RequestID: safeHeader(response.Header.Get("X-GitHub-Request-Id")), ETag: safeHeader(response.Header.Get("ETag")), EnterpriseVersion: safeHeader(response.Header.Get("X-GitHub-Enterprise-Version")), Next: linkValue(response.Header.Get("Link"), "next"), Previous: linkValue(response.Header.Get("Link"), "prev"), RateLimitRemain: -1}
	if raw := response.Header.Get("X-RateLimit-Remaining"); raw != "" {
		meta.RateLimitRemain, _ = strconv.ParseInt(raw, 10, 64)
	}
	if raw := response.Header.Get("X-RateLimit-Reset"); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
			meta.RateLimitReset = time.Unix(seconds, 0).UTC()
		}
	}
	meta.RetryAfter = parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	return meta
}

func safeHeader(value string) string {
	if len(value) > 256 || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return value
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 && seconds <= 24*60*60 {
		return time.Duration(seconds) * time.Second
	}
	if parsed, err := http.ParseTime(value); err == nil && parsed.After(now) {
		return parsed.Sub(now)
	}
	return 0
}

func linkValue(header, relation string) string {
	for _, item := range strings.Split(header, ",") {
		parts := strings.SplitN(strings.TrimSpace(item), ";", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "<") || !strings.HasSuffix(parts[0], ">") {
			continue
		}
		if strings.Contains(parts[1], `rel="`+relation+`"`) {
			return safeHeader(strings.TrimSuffix(strings.TrimPrefix(parts[0], "<"), ">"))
		}
	}
	return ""
}

// GitHubAppTokenConfig creates a least-privilege installation-token source.
// PrivateKey is accepted in memory only; this API deliberately has no path or
// environment-variable variant.
type GitHubAppTokenConfig struct {
	BaseURL           string
	AppID             int64
	InstallationID    int64
	PrivateKey        *rsa.PrivateKey
	RepositoryIDs     []int64
	Permissions       map[string]string
	HTTPClient        *http.Client
	AllowInsecureHTTP bool
}

// GitHubAppTokenSource refreshes an installation token before expiry.
type GitHubAppTokenSource struct {
	baseURL        *url.URL
	appID          int64
	installationID int64
	privateKey     *rsa.PrivateKey
	repositoryIDs  []int64
	permissions    map[string]string
	client         *http.Client
	mu             sync.Mutex
	token          string
	expiresAt      time.Time
}

// NewGitHubAppTokenSource creates an in-memory installation-token source.
func NewGitHubAppTokenSource(config GitHubAppTokenConfig) (*GitHubAppTokenSource, error) {
	if config.AppID <= 0 || config.InstallationID <= 0 || config.PrivateKey == nil {
		return nil, errors.New("GitHub App ID, installation ID, and private key are required")
	}
	base, err := url.Parse(config.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil {
		return nil, errors.New("invalid GitHub App base URL")
	}
	if base.Scheme != "https" && !config.AllowInsecureHTTP {
		return nil, errors.New("GitHub App token endpoint requires HTTPS")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/"
	repositoryIDs := append([]int64(nil), config.RepositoryIDs...)
	for _, id := range repositoryIDs {
		if id <= 0 {
			return nil, errors.New("invalid GitHub App repository ID")
		}
	}
	permissions := make(map[string]string, len(config.Permissions))
	for name, level := range config.Permissions {
		if !validSimpleIdentity(name) || (level != "read" && level != "write") {
			return nil, errors.New("invalid GitHub App permission")
		}
		permissions[name] = level
	}
	return &GitHubAppTokenSource{baseURL: base, appID: config.AppID, installationID: config.InstallationID, privateKey: config.PrivateKey, repositoryIDs: repositoryIDs, permissions: permissions, client: chooseHTTPClient(config.HTTPClient)}, nil
}

// NewGitHubAppTokenSourceFromPEM parses a PEM key into memory and then uses
// the same least-privilege token source. The PEM bytes are not persisted.
func NewGitHubAppTokenSourceFromPEM(config GitHubAppTokenConfig, raw []byte) (*GitHubAppTokenSource, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("invalid GitHub App private key PEM")
	}
	var key *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		key = parsed
	} else if parsedAny, pkcs8Err := x509.ParsePKCS8PrivateKey(block.Bytes); pkcs8Err == nil {
		var ok bool
		key, ok = parsedAny.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("GitHub App private key is not RSA")
		}
	} else {
		return nil, errors.New("invalid GitHub App private key")
	}
	config.PrivateKey = key
	return NewGitHubAppTokenSource(config)
}

func (s *GitHubAppTokenSource) Token(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", errors.New("token context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Until(s.expiresAt) > time.Minute {
		return s.token, nil
	}
	token, expiresAt, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	s.token, s.expiresAt = token, expiresAt
	return token, nil
}

// Clear drops the in-memory cached token after revocation or an explicit
// operator response. It cannot affect an already-issued GitHub token.
func (s *GitHubAppTokenSource) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token, s.expiresAt = "", time.Time{}
}

func (s *GitHubAppTokenSource) fetch(ctx context.Context) (string, time.Time, error) {
	assertion, err := appJWT(s.appID, s.privateKey, time.Now())
	if err != nil {
		return "", time.Time{}, err
	}
	endpoint := *s.baseURL
	endpoint.Path = path.Join(s.baseURL.Path, "app/installations", strconv.FormatInt(s.installationID, 10), "access_tokens")
	payload := map[string]any{"repository_ids": s.repositoryIDs, "permissions": s.permissions}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", time.Time{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(body)))
	if err != nil {
		return "", time.Time{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", GitHubRESTAPIVersion)
	request.Header.Set("Authorization", "Bearer "+assertion)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return "", time.Time{}, &ProviderError{Kind: KindOffline, Err: ErrOffline}
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, defaultRESTBodyLimit+1))
	if readErr != nil || int64(len(data)) > defaultRESTBodyLimit {
		return "", time.Time{}, ErrResponseLimit
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return "", time.Time{}, ErrAuth
		}
		return "", time.Time{}, &RESTError{StatusCode: response.StatusCode}
	}
	var result struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Token == "" {
		return "", time.Time{}, errors.New("invalid GitHub App token response")
	}
	expiresAt, err := time.Parse(time.RFC3339, result.ExpiresAt)
	if err != nil || !expiresAt.After(time.Now()) {
		return "", time.Time{}, errors.New("invalid GitHub App token expiry")
	}
	return result.Token, expiresAt, nil
}

func appJWT(appID int64, key *rsa.PrivateKey, now time.Time) (string, error) {
	if appID <= 0 || key == nil {
		return "", errors.New("invalid GitHub App signing configuration")
	}
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("encode GitHub App assertion header: %w", err)
	}
	claims, err := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": appID})
	if err != nil {
		return "", fmt.Errorf("encode GitHub App assertion claims: %w", err)
	}
	encoding := base64.RawURLEncoding
	message := encoding.EncodeToString(header) + "." + encoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign GitHub App assertion: %w", err)
	}
	return message + "." + encoding.EncodeToString(signature), nil
}
