package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

type Call struct{ Operation, Owner, Name, Visibility, Remote, SHA, Ref string }
type Provider interface {
	EnsureRemote(context.Context, string, string, string) (string, error)
	Push(context.Context, string, string, string) error
	InspectRef(context.Context, string, string) (string, error)
}
type Fake struct {
	mu    sync.Mutex
	repos map[string]string
	refs  map[string]string
	calls []Call
}

func NewFake() *Fake { return &Fake{repos: map[string]string{}, refs: map[string]string{}} }
func (f *Fake) Add(remote, visibility string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos[remote] = visibility
}
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	copy(out, f.calls)
	return out
}
func (f *Fake) EnsureRemote(_ context.Context, owner, name, visibility string) (string, error) {
	if !valid(owner) || !valid(name) || (visibility != "private" && visibility != "public") {
		return "", errors.New("invalid remote identity")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	remote := owner + "/" + name
	f.calls = append(f.calls, Call{Operation: "create", Owner: owner, Name: name, Visibility: visibility, Remote: remote})
	if _, ok := f.repos[remote]; ok {
		return "", fmt.Errorf("remote collision")
	}
	f.repos[remote] = visibility
	return remote, nil
}

var shaRE = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var refRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

func (f *Fake) Push(_ context.Context, remote, sha, ref string) error {
	if !validRemote(remote) || !validSHA(sha) || !validRef(ref) {
		return errors.New("invalid push intent")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.repos[remote]; !ok {
		return errors.New("unknown remote")
	}
	if existing, ok := f.refs[remote+"#"+ref]; ok {
		if existing != sha {
			return ErrRefConflict
		}
		return nil
	}
	f.refs[remote+"#"+ref] = sha
	f.calls = append(f.calls, Call{Operation: "push", Remote: remote, SHA: sha, Ref: ref})
	return nil
}
func (f *Fake) InspectRef(_ context.Context, remote, ref string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sha, ok := f.refs[remote+"#"+ref]
	if !ok {
		return "", errors.New("ref not found")
	}
	return sha, nil
}
func (f *Fake) Ref(ref string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, sha := range f.refs {
		if strings.HasSuffix(key, "#"+ref) {
			return sha
		}
	}
	return ""
}
func valid(s string) bool {
	if s == "" || len(s) > 100 || strings.Contains(s, "@{") || strings.Contains(s, "..") || strings.HasSuffix(strings.ToLower(s), ".lock") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(s)
}

func validRemote(remote string) bool {
	parts := strings.Split(remote, "/")
	return len(parts) == 2 && valid(parts[0]) && valid(parts[1])
}

func validSHA(sha string) bool { return shaRE.MatchString(sha) }
