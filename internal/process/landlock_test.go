package process

import (
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
