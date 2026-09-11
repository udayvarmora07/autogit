package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLandlockABIProbeIsNonMutatingAndExplicit(t *testing.T) {
	version, err := LandlockABI()
	if runtime.GOOS == "linux" {
		if err == nil && version < 1 {
			t.Fatalf("Landlock reported invalid ABI version %d", version)
		}
		if err != nil && version != 0 {
			t.Fatalf("Landlock returned version %d with error %v", version, err)
		}
		return
	}
	if err == nil || version != 0 {
		t.Fatalf("non-Linux Landlock probe=%d, %v; want unavailable", version, err)
	}
}

func TestLandlockDirectEnforcement(t *testing.T) {
	if os.Getenv("AUTOGIT_LANDLOCK_DIRECT_PROBE") == "1" {
		allowed, denied := os.Getenv("AUTOGIT_LANDLOCK_ALLOWED"), os.Getenv("AUTOGIT_LANDLOCK_DENIED")
		if err := enforceLandlock([]string{allowed}, false); err != nil {
			t.Fatalf("enforce Landlock: %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(allowed, "allowed.txt")); err != nil || string(data) != "allowed" {
			t.Fatalf("allowed file read=%q err=%v", data, err)
		}
		if _, err := os.ReadFile(filepath.Join(denied, "denied.txt")); err == nil {
			t.Fatal("Landlock permitted an unlisted hierarchy")
		}
		return
	}
	if !LandlockAvailable() {
		t.Skip("Landlock is unavailable")
	}
	allowed := t.TempDir()
	denied := t.TempDir()
	if err := os.WriteFile(filepath.Join(allowed, "allowed.txt"), []byte("allowed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(denied, "denied.txt"), []byte("denied"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestLandlockDirectEnforcement$")
	command.Env = append(os.Environ(),
		"AUTOGIT_LANDLOCK_DIRECT_PROBE=1",
		"AUTOGIT_LANDLOCK_ALLOWED="+allowed,
		"AUTOGIT_LANDLOCK_DENIED="+denied,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Landlock child probe: %v\n%s", err, output)
	}
}
