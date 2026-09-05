package coordinator

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"autogit/internal/state"
)

// TestCommitProcessBoundarySchedules exercises the durable commit protocol
// through a real test-process exit. The existing fault matrices cover many
// more schedules in-process; these schedules prove that SQLite state and the
// effect evidence survive an actual process boundary.
func TestCommitProcessBoundarySchedules(t *testing.T) {
	if os.Getenv("AUTOGIT_COORDINATOR_HELPER") == "1" {
		return
	}
	points := []string{"after_intent", "after_git", "after_result", "none"}
	for schedule, point := range points {
		t.Run(point, func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(root, "state.db")
			effectPath := filepath.Join(root, "effects")
			id := "process-recovery-" + strconv.Itoa(schedule)
			env := append(os.Environ(),
				"AUTOGIT_COORDINATOR_HELPER=1",
				"AUTOGIT_COORDINATOR_STATE="+statePath,
				"AUTOGIT_COORDINATOR_EFFECT="+effectPath,
				"AUTOGIT_COORDINATOR_ID="+id,
				"AUTOGIT_COORDINATOR_POINT="+point,
			)
			cmd := exec.Command(os.Args[0], "-test.run=^TestCommitProcessBoundaryHelper$", "-test.count=1")
			cmd.Env = env
			if err := cmd.Run(); point == "none" {
				if err != nil {
					t.Fatalf("successful child exited with error: %v", err)
				}
			} else if err == nil {
				t.Fatalf("crash point %q did not terminate the child", point)
			}

			db, err := state.Open(statePath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			req := validCommitRequest(id)
			if point == "after_intent" {
				// The first restart reconciles the durable intent as having no
				// observable Git effect; the retry may then safely execute it.
				if err := (Coordinator{Store: NewStateStore(db), Git: processGit{effectPath: effectPath, sha: processSHA}}).Commit(context.Background(), req); err != nil {
					t.Fatal(err)
				}
			}
			if err := (Coordinator{Store: NewStateStore(db), Git: processGit{effectPath: effectPath, sha: processSHA}}).Commit(context.Background(), req); err != nil {
				t.Fatalf("restart recovery: %v", err)
			}
			data, err := os.ReadFile(effectPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(data), "commit\n"); got != 1 {
				t.Fatalf("Git effect count=%d, want one", got)
			}
			status, sha, _, err := NewStateStore(db).CommitStatus(context.Background(), id)
			if err != nil || status != state.CommitCreated || sha != processSHA {
				t.Fatalf("durable commit status=%q sha=%q err=%v", status, sha, err)
			}
		})
	}
}

func TestConcurrentCommitProcessSchedulesConvergeOnOneEffect(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.db")
	effectPath := filepath.Join(root, "effects")
	id := "process-concurrent"
	commands := make([]*exec.Cmd, 0, 2)
	for worker := 0; worker < 2; worker++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCommitProcessBoundaryHelper$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			"AUTOGIT_COORDINATOR_HELPER=1",
			"AUTOGIT_COORDINATOR_STATE="+statePath,
			"AUTOGIT_COORDINATOR_EFFECT="+effectPath,
			"AUTOGIT_COORDINATOR_ID="+id,
			"AUTOGIT_COORDINATOR_POINT=none",
			"AUTOGIT_COORDINATOR_OWNER=worker-"+strconv.Itoa(worker),
		)
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("concurrent child failed: %v", err)
		}
	}
	data, err := os.ReadFile(effectPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "commit\n"); got != 1 {
		t.Fatalf("concurrent Git effect count=%d, want one", got)
	}
}

func TestSeededRandomizedCoordinatorProcessBoundarySchedules(t *testing.T) {
	if os.Getenv("AUTOGIT_COORDINATOR_HELPER") == "1" || os.Getenv("AUTOGIT_PUSH_HELPER") == "1" {
		return
	}
	const schedules = processScheduleCount
	rng := rand.New(rand.NewSource(0xD017))
	commitPoints := []string{"after_intent", "after_git", "after_result", "none"}
	pushPoints := []string{"after_intent", "after_provider", "after_result", "none"}
	seen := map[string]bool{}
	for schedule := 0; schedule < schedules; schedule++ {
		if rng.Intn(2) == 0 {
			point := commitPoints[rng.Intn(len(commitPoints))]
			seen["commit/"+point] = true
			runRandomCommitProcessSchedule(t, schedule, point)
			continue
		}
		point := pushPoints[rng.Intn(len(pushPoints))]
		seen["push/"+point] = true
		runRandomPushProcessSchedule(t, schedule, point)
	}
	for _, point := range commitPoints {
		if !seen["commit/"+point] {
			t.Fatalf("seeded schedule did not cover commit point %q", point)
		}
	}
	for _, point := range pushPoints {
		if !seen["push/"+point] {
			t.Fatalf("seeded schedule did not cover push point %q", point)
		}
	}
}

func runRandomCommitProcessSchedule(t *testing.T, schedule int, point string) {
	t.Helper()
	root := t.TempDir()
	statePath := filepath.Join(root, "state.db")
	effectPath := filepath.Join(root, "effects")
	id := "random-commit-" + strconv.Itoa(schedule)
	cmd := exec.Command(os.Args[0], "-test.run=^TestCommitProcessBoundaryHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"AUTOGIT_COORDINATOR_HELPER=1",
		"AUTOGIT_COORDINATOR_STATE="+statePath,
		"AUTOGIT_COORDINATOR_EFFECT="+effectPath,
		"AUTOGIT_COORDINATOR_ID="+id,
		"AUTOGIT_COORDINATOR_POINT="+point,
	)
	err := cmd.Run()
	if point == "none" {
		if err != nil {
			t.Fatalf("random commit schedule %d: child: %v", schedule, err)
		}
	} else if err == nil {
		t.Fatalf("random commit schedule %d: crash point %q did not terminate child", schedule, point)
	}
	db, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := Coordinator{Store: NewStateStore(db), Git: processGit{effectPath: effectPath, sha: processSHA}}
	req := validCommitRequest(id)
	if point == "after_intent" {
		if err := coord.Commit(context.Background(), req); err != nil {
			t.Fatalf("random commit schedule %d: first recovery: %v", schedule, err)
		}
	}
	if err := coord.Commit(context.Background(), req); err != nil {
		t.Fatalf("random commit schedule %d: final recovery: %v", schedule, err)
	}
	if got := strings.Count(string(readEffect(effectPath)), "commit\n"); got != 1 {
		t.Fatalf("random commit schedule %d: effects=%d, want one", schedule, got)
	}
	status, sha, _, err := NewStateStore(db).CommitStatus(context.Background(), id)
	if err != nil || status != state.CommitCreated || sha != processSHA {
		t.Fatalf("random commit schedule %d: status=%q sha=%q err=%v", schedule, status, sha, err)
	}
}

func runRandomPushProcessSchedule(t *testing.T, schedule int, point string) {
	t.Helper()
	root := t.TempDir()
	statePath := filepath.Join(root, "state.db")
	effectPath := filepath.Join(root, "push-effects")
	id := "random-push-" + strconv.Itoa(schedule)
	cmd := exec.Command(os.Args[0], "-test.run=^TestPushProcessBoundaryHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"AUTOGIT_PUSH_HELPER=1",
		"AUTOGIT_PUSH_STATE="+statePath,
		"AUTOGIT_PUSH_EFFECT="+effectPath,
		"AUTOGIT_PUSH_ID="+id,
		"AUTOGIT_PUSH_POINT="+point,
	)
	err := cmd.Run()
	if point == "none" {
		if err != nil {
			t.Fatalf("random push schedule %d: child: %v", schedule, err)
		}
	} else if err == nil {
		t.Fatalf("random push schedule %d: crash point %q did not terminate child", schedule, point)
	}
	db, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := Coordinator{Store: NewStateStore(db), Provider: processProvider{effectPath: effectPath}}
	if err := coord.Push(context.Background(), processPushRequest(id)); err != nil {
		t.Fatalf("random push schedule %d: final recovery: %v", schedule, err)
	}
	if got := strings.Count(string(readEffect(effectPath)), "push\n"); got != 1 {
		t.Fatalf("random push schedule %d: effects=%d, want one", schedule, got)
	}
	status, _, err := NewStateStore(db).PushStatus(context.Background(), id)
	if err != nil || status != state.PushSucceeded {
		t.Fatalf("random push schedule %d: status=%q err=%v", schedule, status, err)
	}
}

func TestPushProcessBoundarySchedules(t *testing.T) {
	if os.Getenv("AUTOGIT_PUSH_HELPER") == "1" {
		return
	}
	points := []string{"after_intent", "after_provider", "after_result", "none"}
	for schedule, point := range points {
		t.Run(point, func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(root, "state.db")
			effectPath := filepath.Join(root, "push-effects")
			id := "push-process-recovery-" + strconv.Itoa(schedule)
			cmd := exec.Command(os.Args[0], "-test.run=^TestPushProcessBoundaryHelper$", "-test.count=1")
			cmd.Env = append(os.Environ(),
				"AUTOGIT_PUSH_HELPER=1",
				"AUTOGIT_PUSH_STATE="+statePath,
				"AUTOGIT_PUSH_EFFECT="+effectPath,
				"AUTOGIT_PUSH_ID="+id,
				"AUTOGIT_PUSH_POINT="+point,
			)
			if err := cmd.Run(); point == "none" {
				if err != nil {
					t.Fatalf("successful child exited with error: %v", err)
				}
			} else if err == nil {
				t.Fatalf("crash point %q did not terminate the child", point)
			}

			db, err := state.Open(statePath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			req := processPushRequest(id)
			if err := (Coordinator{Store: NewStateStore(db), Provider: processProvider{effectPath: effectPath}}).Push(context.Background(), req); err != nil {
				t.Fatalf("restart recovery: %v", err)
			}
			data, err := os.ReadFile(effectPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(data), "push\n"); got != 1 {
				t.Fatalf("provider effect count=%d, want one", got)
			}
			status, _, err := NewStateStore(db).PushStatus(context.Background(), id)
			if err != nil || status != state.PushSucceeded {
				t.Fatalf("durable push status=%q err=%v", status, err)
			}
		})
	}
}

func TestPushProcessBoundaryHelper(t *testing.T) {
	if os.Getenv("AUTOGIT_PUSH_HELPER") != "1" {
		return
	}
	statePath := os.Getenv("AUTOGIT_PUSH_STATE")
	effectPath := os.Getenv("AUTOGIT_PUSH_EFFECT")
	id := os.Getenv("AUTOGIT_PUSH_ID")
	point := os.Getenv("AUTOGIT_PUSH_POINT")
	db, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &crashingPushStore{StateStore: NewStateStore(db), point: point}
	coord := Coordinator{Store: store, Provider: processProvider{effectPath: effectPath, point: point}}
	if err := coord.Push(context.Background(), processPushRequest(id)); err != nil {
		t.Fatal(err)
	}
}

func processPushRequest(id string) PushRequest {
	return PushRequest{ID: id, Owner: "owner", Name: "repo", Ref: "main", CommitSHA: strings.Repeat("a", 40), RemoteDigest: "sha256:" + strings.Repeat("b", 64)}
}

type processProvider struct {
	effectPath string
	point      string
}

func (p processProvider) ConfirmPush(context.Context, PushRequest) (ConfirmPushOutcome, error) {
	if len(readEffect(p.effectPath)) > 0 {
		return PushPresent, nil
	}
	return PushMissing, nil
}

func (p processProvider) Push(context.Context, PushRequest) error {
	file, err := os.OpenFile(p.effectPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString("push\n"); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if p.point == "after_provider" {
		os.Exit(73)
	}
	return nil
}

type crashingPushStore struct {
	*StateStore
	point string
}

func (s *crashingPushStore) PutPushIntent(ctx context.Context, req PushRequest) error {
	if err := s.StateStore.PutPushIntent(ctx, req); err != nil {
		return err
	}
	if s.point == "after_intent" {
		os.Exit(73)
	}
	return nil
}

func (s *crashingPushStore) MarkPushSucceeded(ctx context.Context, id string) error {
	if err := s.StateStore.MarkPushSucceeded(ctx, id); err != nil {
		return err
	}
	if s.point == "after_result" {
		os.Exit(73)
	}
	return nil
}

func TestCommitProcessBoundaryHelper(t *testing.T) {
	if os.Getenv("AUTOGIT_COORDINATOR_HELPER") != "1" {
		return
	}
	statePath := os.Getenv("AUTOGIT_COORDINATOR_STATE")
	effectPath := os.Getenv("AUTOGIT_COORDINATOR_EFFECT")
	id := os.Getenv("AUTOGIT_COORDINATOR_ID")
	point := os.Getenv("AUTOGIT_COORDINATOR_POINT")
	owner := os.Getenv("AUTOGIT_COORDINATOR_OWNER")
	db, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &crashingCommitStore{StateStore: NewStateStore(db), point: point}
	git := processGit{effectPath: effectPath, sha: processSHA, point: point}
	coordinator := Coordinator{Store: store, Git: git}
	if owner != "" {
		coordinator.Lease = StateLease{DB: db}
		coordinator.Owner = owner
	}
	if err := coordinator.Commit(context.Background(), validCommitRequest(id)); err != nil {
		t.Fatal(err)
	}
}

const processSHA = "0123456789abcdef0123456789abcdef01234567"

type processGit struct {
	effectPath string
	sha        string
	point      string
}

func (g processGit) Commit(context.Context, CommitRequest) (string, error) {
	file, err := os.OpenFile(g.effectPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return "", err
	}
	if _, err := file.WriteString("commit\n"); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if g.point == "after_git" {
		os.Exit(73)
	}
	return g.sha, nil
}

func (g processGit) Inspect(context.Context, CommitRequest) (string, error) {
	if len(readEffect(g.effectPath)) == 0 {
		return "", errors.New("commit effect is absent")
	}
	return g.sha, nil
}

func readEffect(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}

type crashingCommitStore struct {
	*StateStore
	point string
}

func (s *crashingCommitStore) PutCommitIntent(ctx context.Context, req CommitRequest) error {
	if err := s.StateStore.PutCommitIntent(ctx, req); err != nil {
		return err
	}
	if s.point == "after_intent" {
		os.Exit(73)
	}
	return nil
}

func (s *crashingCommitStore) RecordCommit(ctx context.Context, id, sha string) error {
	if err := s.StateStore.RecordCommit(ctx, id, sha); err != nil {
		return err
	}
	if s.point == "after_result" {
		os.Exit(73)
	}
	return nil
}
