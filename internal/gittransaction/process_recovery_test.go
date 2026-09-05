package gittransaction

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"autogit/internal/state"
)

func TestGitTransactionProcessBoundarySchedules(t *testing.T) {
	if os.Getenv("AUTOGIT_GITTX_HELPER") == "1" {
		return
	}
	for _, point := range []string{"after_ref", "after_result", "after_intent"} {
		t.Run(point, func(t *testing.T) {
			repo := t.TempDir()
			if output, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, output)
			}
			statePath := filepath.Join(t.TempDir(), "state.db")
			logPath := filepath.Join(t.TempDir(), "git-effects")
			cmd := exec.Command(os.Args[0], "-test.run=^TestGitTransactionProcessBoundaryHelper$", "-test.count=1")
			cmd.Env = append(os.Environ(),
				"AUTOGIT_GITTX_HELPER=1",
				"AUTOGIT_GITTX_REPO="+repo,
				"AUTOGIT_GITTX_STATE="+statePath,
				"AUTOGIT_GITTX_LOG="+logPath,
				"AUTOGIT_GITTX_POINT="+point,
			)
			if err := cmd.Run(); err == nil {
				t.Fatalf("crash point %q did not terminate the child", point)
			}

			db, err := state.Open(statePath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			port := NewStateIntentPort(db)
			req := processGitRequest(repo)
			if point == "after_intent" {
				if _, err := New(SystemRunner{}, port).Recover(context.Background(), req.ID); err == nil {
					t.Fatal("pre-effect intent unexpectedly recovered as committed")
				}
				record, recordErr := db.GitCommitIntentRecord(context.Background(), req.ID)
				if recordErr != nil || record.State != state.CommitIntentReconcile {
					t.Fatalf("pre-effect record=%+v err=%v", record, recordErr)
				}
				if got := strings.TrimSpace(gitCommandOutput(repo, "show-ref", "--heads")); got != "" {
					t.Fatalf("pre-effect crash created a ref: %q", got)
				}
				return
			}
			got, err := New(SystemRunner{}, port).Recover(context.Background(), req.ID)
			if err != nil || got.SHA == "" {
				t.Fatalf("restart recovery commit=%+v err=%v", got, err)
			}
			if count := strings.Count(string(readGitEffect(logPath)), "commit-tree\n"); count != 1 {
				t.Fatalf("commit-tree effect count=%d, want one", count)
			}
		})
	}
}

func TestSeededRandomizedGitTransactionProcessBoundarySchedules(t *testing.T) {
	if os.Getenv("AUTOGIT_GITTX_HELPER") == "1" {
		return
	}
	const schedules = 1000
	points := []string{"after_intent", "after_ref", "after_result", "none"}
	rng := rand.New(rand.NewSource(0xF017))
	seen := map[string]bool{}
	for schedule := 0; schedule < schedules; schedule++ {
		point := points[rng.Intn(len(points))]
		seen[point] = true
		runRandomGitTransactionProcessSchedule(t, schedule, point)
	}
	for _, point := range points {
		if !seen[point] {
			t.Fatalf("seeded schedule did not cover Git transaction point %q", point)
		}
	}
}

func runRandomGitTransactionProcessSchedule(t *testing.T, schedule int, point string) {
	t.Helper()
	repo := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	statePath := filepath.Join(t.TempDir(), "state.db")
	logPath := filepath.Join(t.TempDir(), "git-effects")
	cmd := exec.Command(os.Args[0], "-test.run=^TestGitTransactionProcessBoundaryHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"AUTOGIT_GITTX_HELPER=1",
		"AUTOGIT_GITTX_REPO="+repo,
		"AUTOGIT_GITTX_STATE="+statePath,
		"AUTOGIT_GITTX_LOG="+logPath,
		"AUTOGIT_GITTX_POINT="+point,
	)
	err := cmd.Run()
	if point == "none" {
		if err != nil {
			t.Fatalf("random Git transaction schedule %d: child: %v", schedule, err)
		}
	} else if err == nil {
		t.Fatalf("random Git transaction schedule %d: crash point %q did not terminate child", schedule, point)
	}
	db, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	port := NewStateIntentPort(db)
	req := processGitRequest(repo)
	if point == "after_intent" {
		if _, err := New(SystemRunner{}, port).Recover(context.Background(), req.ID); err == nil {
			t.Fatalf("random Git transaction schedule %d: pre-effect intent recovered as committed", schedule)
		}
		record, recordErr := db.GitCommitIntentRecord(context.Background(), req.ID)
		if recordErr != nil || record.State != state.CommitIntentReconcile {
			t.Fatalf("random Git transaction schedule %d: record=%+v err=%v", schedule, record, recordErr)
		}
		if got := strings.TrimSpace(gitCommandOutput(repo, "show-ref", "--heads")); got != "" {
			t.Fatalf("random Git transaction schedule %d: pre-effect crash created a ref: %q", schedule, got)
		}
		return
	}
	got, err := New(SystemRunner{}, port).Recover(context.Background(), req.ID)
	if err != nil || got.SHA == "" {
		t.Fatalf("random Git transaction schedule %d: recovery commit=%+v err=%v", schedule, got, err)
	}
	if count := strings.Count(string(readGitEffect(logPath)), "commit-tree\n"); count != 1 {
		t.Fatalf("random Git transaction schedule %d: commit-tree effects=%d, want one", schedule, count)
	}
}

func TestGitTransactionProcessBoundaryHelper(t *testing.T) {
	if os.Getenv("AUTOGIT_GITTX_HELPER") != "1" {
		return
	}
	repo := os.Getenv("AUTOGIT_GITTX_REPO")
	statePath := os.Getenv("AUTOGIT_GITTX_STATE")
	logPath := os.Getenv("AUTOGIT_GITTX_LOG")
	point := os.Getenv("AUTOGIT_GITTX_POINT")
	db, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	port := &crashingIntentPort{inner: NewStateIntentPort(db), point: point}
	runner := crashingGitRunner{base: SystemRunner{}, logPath: logPath, point: point}
	if _, err := New(runner, port).Create(context.Background(), processGitRequest(repo)); err != nil {
		t.Fatal(err)
	}
}

func processGitRequest(repo string) Request {
	return Request{ID: "git-process-recovery", RepoDir: repo, Snapshot: []SnapshotEntry{{Path: "candidate.txt", Content: []byte("candidate\n"), Mode: 0644}}, Message: "feat: recover process boundary", PolicyDigest: emptyDigest(), VerifierDigest: emptyDigest(), GuardDigest: emptyDigest()}
}

type crashingIntentPort struct {
	inner IntentPort
	point string
}

func (p *crashingIntentPort) PutCommitIntent(ctx context.Context, intent Intent) error {
	if err := p.inner.PutCommitIntent(ctx, intent); err != nil {
		return err
	}
	if p.point == "after_intent" {
		os.Exit(73)
	}
	return nil
}

func (p *crashingIntentPort) GetCommitIntent(ctx context.Context, id string) (Intent, error) {
	return p.inner.GetCommitIntent(ctx, id)
}

func (p *crashingIntentPort) RecordCommit(ctx context.Context, id, sha string) error {
	if err := p.inner.RecordCommit(ctx, id, sha); err != nil {
		return err
	}
	if p.point == "after_result" {
		os.Exit(73)
	}
	return nil
}

func (p *crashingIntentPort) RecordReconcile(ctx context.Context, id, reason string) error {
	return p.inner.RecordReconcile(ctx, id, reason)
}

type crashingGitRunner struct {
	base    Runner
	logPath string
	point   string
}

func (r crashingGitRunner) Run(ctx context.Context, dir string, env map[string]string, args ...string) (Result, error) {
	result, err := r.base.Run(ctx, dir, env, args...)
	if containsGitArg(args, "commit-tree") && err == nil {
		if logErr := appendGitEffect(r.logPath, "commit-tree\n"); logErr != nil {
			return Result{}, logErr
		}
	}
	if containsGitArg(args, "update-ref") && err == nil && r.point == "after_ref" {
		os.Exit(73)
	}
	return result, err
}

func appendGitEffect(path, value string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(value); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readGitEffect(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}

func gitCommandOutput(repo string, args ...string) string {
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, _ := command.CombinedOutput()
	return string(output)
}

func containsGitArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
