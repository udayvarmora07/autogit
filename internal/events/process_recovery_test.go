package events

import (
	"bytes"
	"context"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeededRandomizedEventReceiptProcessSchedules(t *testing.T) {
	if os.Getenv("AUTOGIT_EVENT_HELPER") == "1" {
		return
	}
	const schedules = randomizedProcessScheduleCount
	points := []string{"before_receipt", "after_receipt", "none", "concurrent"}
	rng := rand.New(rand.NewSource(0xE7E17))
	seen := map[string]bool{}
	for schedule := 0; schedule < schedules; schedule++ {
		point := points[rng.Intn(len(points))]
		seen[point] = true
		runEventReceiptProcessSchedule(t, schedule, point)
	}
	for _, point := range points {
		if !seen[point] {
			t.Fatalf("seeded schedule did not cover event receipt point %q", point)
		}
	}
}

func runEventReceiptProcessSchedule(t *testing.T, schedule int, point string) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "events.db")
	start := func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestEventReceiptProcessBoundaryHelper$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			"AUTOGIT_EVENT_HELPER=1",
			"AUTOGIT_EVENT_STATE="+dbPath,
			"AUTOGIT_EVENT_POINT="+point,
		)
		return cmd
	}
	if point == "concurrent" {
		first, second := start(), start()
		var firstOutput, secondOutput bytes.Buffer
		first.Stdout, first.Stderr = &firstOutput, &firstOutput
		second.Stdout, second.Stderr = &secondOutput, &secondOutput
		if err := first.Start(); err != nil {
			t.Fatalf("event schedule %d first start: %v", schedule, err)
		}
		if err := second.Start(); err != nil {
			t.Fatalf("event schedule %d second start: %v", schedule, err)
		}
		if err := first.Wait(); err != nil {
			t.Fatalf("event schedule %d first child: %v: %s", schedule, err, firstOutput.String())
		}
		if err := second.Wait(); err != nil {
			t.Fatalf("event schedule %d second child: %v: %s", schedule, err, secondOutput.String())
		}
	} else {
		cmd := start()
		err := cmd.Run()
		if point == "none" {
			if err != nil {
				t.Fatalf("event schedule %d child: %v", schedule, err)
			}
		} else if err == nil {
			t.Fatalf("event schedule %d crash point %q did not terminate child", schedule, point)
		}
	}
	s, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err := Decode([]byte(validEvent), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Accept(context.Background(), e)
	if err != nil || got.Disposition != Duplicate && got.Disposition != Accepted {
		t.Fatalf("event schedule %d recovery receipt=%+v err=%v", schedule, got, err)
	}
	var receipts int
	if err := s.db.QueryRow(`SELECT count(*) FROM event_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("event schedule %d receipts=%d, want one", schedule, receipts)
	}
}

func TestEventReceiptProcessBoundaryHelper(t *testing.T) {
	if os.Getenv("AUTOGIT_EVENT_HELPER") != "1" {
		return
	}
	point := os.Getenv("AUTOGIT_EVENT_POINT")
	if point == "before_receipt" {
		os.Exit(97)
	}
	s, err := OpenStore(os.Getenv("AUTOGIT_EVENT_STATE"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err := Decode([]byte(validEvent), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Accept(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if point == "after_receipt" {
		os.Exit(98)
	}
	if point == "none" || point == "concurrent" {
		return
	}
	t.Fatalf("unknown event schedule point %q", strings.TrimSpace(point))
}
