package provider

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"autogit/internal/state"
)

func TestRepositoryTransactionProcessBoundarySchedules(t *testing.T) {
	if os.Getenv("AUTOGIT_REMOTE_HELPER") == "1" {
		return
	}
	points := []string{"after_intent", "after_hosted", "after_created", "after_attached", "none"}
	for schedule, point := range points {
		t.Run(point, func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(root, "state.db")
			hostedPath := filepath.Join(root, "hosted-effects")
			attachedPath := filepath.Join(root, "attached-effects")
			id := "remote-process-" + strconv.Itoa(schedule)
			cmd := exec.Command(os.Args[0], "-test.run=^TestRepositoryTransactionProcessBoundaryHelper$", "-test.count=1")
			cmd.Env = append(os.Environ(),
				"AUTOGIT_REMOTE_HELPER=1",
				"AUTOGIT_REMOTE_STATE="+statePath,
				"AUTOGIT_REMOTE_HOSTED="+hostedPath,
				"AUTOGIT_REMOTE_ATTACHED="+attachedPath,
				"AUTOGIT_REMOTE_ID="+id,
				"AUTOGIT_REMOTE_POINT="+point,
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
			req := processRemoteRequest(id)
			transaction := RepositoryTransaction{State: db, Hosted: processHosted{path: hostedPath}, Git: processBinder{path: attachedPath}}
			if got, err := transaction.Create(context.Background(), req); err != nil || got != "owner/repo" {
				t.Fatalf("restart recovery identity=%q err=%v", got, err)
			}
			if got := strings.Count(string(readProcessEffect(hostedPath)), "create\n"); got != 1 {
				t.Fatalf("hosted create effect count=%d, want one", got)
			}
			if got := strings.Count(string(readProcessEffect(attachedPath)), "attach\n"); got != 1 {
				t.Fatalf("remote attach effect count=%d, want one", got)
			}
			job, err := db.RemoteJob(id)
			if err != nil || job.State != state.RemoteAttached {
				t.Fatalf("durable remote job=%+v err=%v", job, err)
			}
		})
	}
}

func TestSeededRandomizedRepositoryProcessBoundarySchedules(t *testing.T) {
	if os.Getenv("AUTOGIT_REMOTE_HELPER") == "1" {
		return
	}
	const schedules = processScheduleCount
	points := []string{"after_intent", "after_hosted", "after_created", "after_attached", "none"}
	rng := rand.New(rand.NewSource(0xE017))
	seen := map[string]bool{}
	for schedule := 0; schedule < schedules; schedule++ {
		point := points[rng.Intn(len(points))]
		seen[point] = true
		runRandomRepositoryProcessSchedule(t, schedule, point)
	}
	for _, point := range points {
		if !seen[point] {
			t.Fatalf("seeded schedule did not cover hosted boundary %q", point)
		}
	}
}

func runRandomRepositoryProcessSchedule(t *testing.T, schedule int, point string) {
	t.Helper()
	root := t.TempDir()
	statePath := filepath.Join(root, "state.db")
	hostedPath := filepath.Join(root, "hosted-effects")
	attachedPath := filepath.Join(root, "attached-effects")
	id := "random-remote-" + strconv.Itoa(schedule)
	cmd := exec.Command(os.Args[0], "-test.run=^TestRepositoryTransactionProcessBoundaryHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"AUTOGIT_REMOTE_HELPER=1",
		"AUTOGIT_REMOTE_STATE="+statePath,
		"AUTOGIT_REMOTE_HOSTED="+hostedPath,
		"AUTOGIT_REMOTE_ATTACHED="+attachedPath,
		"AUTOGIT_REMOTE_ID="+id,
		"AUTOGIT_REMOTE_POINT="+point,
	)
	err := cmd.Run()
	if point == "none" {
		if err != nil {
			t.Fatalf("random repository schedule %d: child: %v", schedule, err)
		}
	} else if err == nil {
		t.Fatalf("random repository schedule %d: crash point %q did not terminate child", schedule, point)
	}
	db, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	req := processRemoteRequest(id)
	transaction := RepositoryTransaction{State: db, Hosted: processHosted{path: hostedPath}, Git: processBinder{path: attachedPath}}
	if got, err := transaction.Create(context.Background(), req); err != nil || got != "owner/repo" {
		t.Fatalf("random repository schedule %d: recovery identity=%q err=%v", schedule, got, err)
	}
	if got := strings.Count(string(readProcessEffect(hostedPath)), "create\n"); got != 1 {
		t.Fatalf("random repository schedule %d: hosted creates=%d, want one", schedule, got)
	}
	if got := strings.Count(string(readProcessEffect(attachedPath)), "attach\n"); got != 1 {
		t.Fatalf("random repository schedule %d: attachments=%d, want one", schedule, got)
	}
	job, err := db.RemoteJob(id)
	if err != nil || job.State != state.RemoteAttached || job.HostedIdentity != "owner/repo" {
		t.Fatalf("random repository schedule %d: job=%+v err=%v", schedule, job, err)
	}
}

func TestRepositoryTransactionProcessBoundaryHelper(t *testing.T) {
	if os.Getenv("AUTOGIT_REMOTE_HELPER") != "1" {
		return
	}
	statePath := os.Getenv("AUTOGIT_REMOTE_STATE")
	hostedPath := os.Getenv("AUTOGIT_REMOTE_HOSTED")
	attachedPath := os.Getenv("AUTOGIT_REMOTE_ATTACHED")
	id := os.Getenv("AUTOGIT_REMOTE_ID")
	point := os.Getenv("AUTOGIT_REMOTE_POINT")
	db, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	jobs := &crashingRemoteJobStore{inner: stateRemoteJobStore{db: db}, point: point}
	transaction := RepositoryTransaction{State: db, Jobs: jobs, Hosted: processHosted{path: hostedPath, point: point}, Git: processBinder{path: attachedPath}}
	if _, err := transaction.Create(context.Background(), processRemoteRequest(id)); err != nil {
		t.Fatal(err)
	}
}

func processRemoteRequest(id string) RemoteCreateRequest {
	return RemoteCreateRequest{ID: id, RepositoryID: "sha256:" + strings.Repeat("1", 64), Alias: "origin", Owner: "owner", Name: "repo", Visibility: "private"}
}

type processHosted struct {
	path  string
	point string
}

func (p processHosted) Create(_ context.Context, r RemoteRequest) (string, error) {
	if err := appendProcessEffect(p.path, "create\n"); err != nil {
		return "", err
	}
	if p.point == "after_hosted" {
		os.Exit(73)
	}
	return r.Owner + "/" + r.Name, nil
}

func (p processHosted) ConfirmRepository(context.Context, RemoteRequest) error {
	if len(readProcessEffect(p.path)) == 0 {
		return &ProviderError{Kind: KindAbsent, Err: ErrRefAbsent}
	}
	return nil
}

type processBinder struct{ path string }

func (b processBinder) RemoteURL(context.Context, string) (string, error) {
	if len(readProcessEffect(b.path)) == 0 {
		return "", ErrRemoteAbsent
	}
	return "https://github.com/owner/repo.git", nil
}

func (b processBinder) AddRemote(_ context.Context, _, _ string) error {
	return appendProcessEffect(b.path, "attach\n")
}

func appendProcessEffect(path, value string) error {
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

func readProcessEffect(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}

type crashingRemoteJobStore struct {
	inner RemoteJobStore
	point string
}

func (s *crashingRemoteJobStore) RemoteJob(id string) (state.RemoteJob, error) {
	return s.inner.RemoteJob(id)
}

func (s *crashingRemoteJobStore) PutRemoteJob(ctx context.Context, job state.RemoteJob) error {
	if err := s.inner.PutRemoteJob(ctx, job); err != nil {
		return err
	}
	if s.point == "after_intent" && job.State == state.RemoteRequested {
		os.Exit(73)
	}
	if s.point == "after_created" && job.State == state.RemoteCreated {
		os.Exit(73)
	}
	if s.point == "after_attached" && job.State == state.RemoteAttached {
		os.Exit(73)
	}
	return nil
}
