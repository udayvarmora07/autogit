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
		return IsolationCapability{Tier: tier, Available: true, Enforced: true, Reason: fmt.Sprintf("argv, scrubbed environment, timeout, bounded output, descendant cleanup, and OS process limits where supported on %s", runtime.GOOS)}
	case TierFilesystemIsolated:
		return IsolationCapability{Tier: tier, Reason: "filesystem isolation is not yet implemented; refusing to claim a sandbox"}
	case TierFilesystemNetworkIsolated:
		return IsolationCapability{Tier: tier, Reason: "filesystem and network isolation is not yet implemented; refusing to claim a sandbox"}
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

func defaultVerifierResourceLimits() process.ResourceLimits {
	return process.ResourceLimits{CPUTime: 10 * time.Minute, MemoryBytes: 1 << 30, FileBytes: 64 << 20, Processes: 64}
}
