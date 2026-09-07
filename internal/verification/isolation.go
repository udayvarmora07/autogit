package verification

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	"autogit/internal/process"
)

// IsolationTier is an explicit statement of what the verifier boundary
// enforces. A capability report is part of verification evidence; a caller
// must not infer stronger isolation from a process timeout alone.
type IsolationTier string

const (
	TierNone                      IsolationTier = "none"
	TierProcessBounded            IsolationTier = "process-bounded"
	TierFilesystemIsolated        IsolationTier = "filesystem-isolated"
	TierFilesystemNetworkIsolated IsolationTier = "filesystem-and-network-isolated"
	TierRemoteHermetic            IsolationTier = "remote-hermetic"
)

type IsolationCapability struct {
	Tier      IsolationTier
	Available bool
	Enforced  bool
	Reason    string
}

func DefaultIsolationTier() IsolationTier { return TierProcessBounded }

func CapabilityFor(tier IsolationTier) IsolationCapability {
	switch tier {
	case TierNone:
		return IsolationCapability{Tier: tier, Available: true, Enforced: true, Reason: "no isolation was requested; only an explicit local exception may use this tier"}
	case TierProcessBounded:
		reason := fmt.Sprintf("argv, scrubbed environment, timeout, bounded output, descendant cleanup, and process limits supported on %s", runtime.GOOS)
		if runtime.GOOS == "darwin" {
			reason = "argv, scrubbed environment, timeout, bounded output, and process-group cleanup; macOS resource ceilings are unavailable"
		} else if runtime.GOOS == "windows" {
			reason = "argv, scrubbed environment, timeout, bounded output, descendant cleanup, and Windows job-object CPU/memory/process limits"
		}
		return IsolationCapability{Tier: tier, Available: true, Enforced: true, Reason: reason}
	case TierFilesystemIsolated:
		if runtime.GOOS == "linux" && process.NamespaceSandboxAvailable() {
			return IsolationCapability{Tier: tier, Available: true, Enforced: true, Reason: "Linux bubblewrap user/pid namespaces and read-only filesystem allowlist"}
		}
		return IsolationCapability{Tier: tier, Reason: "filesystem isolation is unavailable on this platform or bubblewrap is not installed"}
	case TierFilesystemNetworkIsolated:
		if runtime.GOOS == "linux" && process.NamespaceSandboxAvailable() {
			return IsolationCapability{Tier: tier, Available: true, Enforced: true, Reason: "Linux bubblewrap user/pid/network namespaces and read-only filesystem allowlist"}
		}
		return IsolationCapability{Tier: tier, Reason: "filesystem and network isolation is unavailable on this platform or bubblewrap is not installed"}
	case TierRemoteHermetic:
		return IsolationCapability{Tier: tier, Reason: "remote hermetic execution is outside this local binary"}
	default:
		return IsolationCapability{Tier: tier, Reason: "unknown isolation tier"}
	}
}

func RequireCapability(tier IsolationTier) (IsolationCapability, error) {
	if tier == "" {
		tier = DefaultIsolationTier()
	}
	capability := CapabilityFor(tier)
	if !capability.Available || !capability.Enforced {
		return capability, errors.New(capability.Reason)
	}
	return capability, nil
}

func defaultVerifierResourceLimits(tier IsolationTier) process.ResourceLimits {
	switch runtime.GOOS {
	case "linux":
		limits := process.ResourceLimits{CPUTime: 10 * time.Minute, MemoryBytes: 4 << 30, FileBytes: 64 << 20}
		// RLIMIT_NPROC is charged to the real UID. Applying it to a normal
		// process-bounded verifier would include unrelated user processes and
		// can prevent the verifier runtime from creating its own threads. A
		// bubblewrap user namespace gives the isolated tiers an independent
		// process accounting domain, so the per-user ceiling is meaningful there.
		if tier == TierFilesystemIsolated || tier == TierFilesystemNetworkIsolated {
			limits.Processes = 64
		}
		return limits
	case "windows":
		return process.ResourceLimits{CPUTime: 10 * time.Minute, MemoryBytes: 1 << 30, Processes: 64}
	default:
		return process.ResourceLimits{}
	}
}
