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
	Tier         IsolationTier          `json:"tier"`
	Available    bool                   `json:"available"`
	Enforced     bool                   `json:"enforced"`
	Reason       string                 `json:"reason"`
	Primitive    string                 `json:"primitive,omitempty"`
	Limitations  []string               `json:"limitations,omitempty"`
	Observations map[string]interface{} `json:"observations,omitempty"`
}

func DefaultIsolationTier() IsolationTier { return TierProcessBounded }

func CapabilityFor(tier IsolationTier) IsolationCapability {
	switch tier {
	case TierNone:
		return IsolationCapability{Tier: tier, Available: true, Enforced: true, Reason: "no isolation was requested; only an explicit local exception may use this tier", Primitive: "none"}
	case TierProcessBounded:
		if !process.ProcessBoundedAvailable() {
			return IsolationCapability{Tier: tier, Reason: fmt.Sprintf("native process supervisor is unavailable on %s", runtime.GOOS), Limitations: []string{"process-bounded execution is unavailable; verification must fail closed"}}
		}
		reason := fmt.Sprintf("argv, scrubbed environment, timeout, bounded output, descendant cleanup, and process limits supported on %s", runtime.GOOS)
		primitive := "process-group"
		limitations := []string(nil)
		if runtime.GOOS == "darwin" {
			reason = "argv, scrubbed environment, timeout, bounded output, and process-group cleanup; macOS resource ceilings are unavailable"
			limitations = []string{"resource ceilings are unavailable; filesystem and network isolation tiers are unavailable"}
		} else if runtime.GOOS == "windows" {
			reason = "argv, scrubbed environment, timeout, bounded output, descendant cleanup, and Windows job-object CPU/memory/process limits"
			primitive = "job-object"
			limitations = []string{"AppContainer is not enabled by this binary; filesystem and network isolation tiers are unavailable"}
		} else if runtime.GOOS == "linux" {
			primitive = "process-group+prlimit"
		}
		return IsolationCapability{Tier: tier, Available: true, Enforced: true, Reason: reason, Primitive: primitive, Limitations: limitations}
	case TierFilesystemIsolated:
		if runtime.GOOS == "linux" && process.NamespaceSandboxAvailable() {
			return linuxFilesystemCapability(tier, false)
		}
		if runtime.GOOS == "windows" && process.AppContainerAvailable() {
			return windowsAppContainerCapability(tier)
		}
		return unavailableFilesystemCapability(tier, "filesystem isolation is unavailable on this platform or bubblewrap is not installed")
	case TierFilesystemNetworkIsolated:
		if runtime.GOOS == "linux" && process.NamespaceSandboxAvailable() {
			return linuxFilesystemCapability(tier, true)
		}
		if runtime.GOOS == "windows" && process.AppContainerAvailable() {
			return windowsAppContainerCapability(tier)
		}
		return unavailableFilesystemCapability(tier, "filesystem and network isolation is unavailable on this platform or bubblewrap is not installed")
	case TierRemoteHermetic:
		return IsolationCapability{Tier: tier, Reason: "remote hermetic execution is outside this local binary"}
	default:
		return IsolationCapability{Tier: tier, Reason: "unknown isolation tier"}
	}
}

func windowsAppContainerCapability(tier IsolationTier) IsolationCapability {
	return IsolationCapability{
		Tier:      tier,
		Available: true,
		Enforced:  true,
		Reason:    "Windows AppContainer explicit filesystem ACLs, low-integrity token, deny-by-default network boundary, parent token attestation, and Job Object cleanup",
		Primitive: "appcontainer+job-object",
		Limitations: []string{
			"the AppContainer launch path grants temporary access only to the explicit allowlist and restores every modified DACL after exit",
		},
		Observations: map[string]interface{}{
			"appcontainer_api":          "available",
			"parent_token_attestation":  "required",
			"network_denied_by_default": true,
		},
	}
}

func linuxFilesystemCapability(tier IsolationTier, networkDisabled bool) IsolationCapability {
	primitive := "bubblewrap-user-pid-namespace"
	reason := "Linux bubblewrap user/pid namespaces and read-only filesystem allowlist"
	if networkDisabled {
		primitive += "+network-namespace"
		reason = "Linux bubblewrap user/pid/network namespaces and read-only filesystem allowlist"
	}
	observations := map[string]interface{}{}
	if abi, err := process.LandlockABI(); err == nil {
		observations["landlock_abi"] = abi
		if process.LandlockAvailable() {
			primitive += "+landlock-ruleset"
			observations["landlock_enforced"] = true
			observations["landlock_reason"] = "the child wrapper installs a deny-by-default in-process Landlock ruleset before exec"
		} else {
			observations["landlock_enforced"] = false
			observations["landlock_reason"] = "kernel Landlock ABI is below the supported enforcement floor; bubblewrap namespace enforcement remains the achieved primitive"
		}
	} else {
		observations["landlock_abi"] = 0
		observations["landlock_enforced"] = false
		observations["landlock_reason"] = "kernel Landlock ABI unavailable; bubblewrap namespace enforcement remains the achieved primitive"
	}
	observations["nested_user_namespaces"] = "disabled-and-asserted"
	return IsolationCapability{Tier: tier, Available: true, Enforced: true, Reason: reason, Primitive: primitive, Observations: observations}
}

func unavailableFilesystemCapability(tier IsolationTier, reason string) IsolationCapability {
	limitations := []string(nil)
	if runtime.GOOS == "windows" {
		limitations = []string{"Windows job objects cover process/resource cleanup; AppContainer is not enabled"}
	} else if runtime.GOOS == "darwin" {
		limitations = []string{"macOS process-group fallback does not provide filesystem or network isolation"}
	}
	return IsolationCapability{Tier: tier, Reason: reason, Limitations: limitations}
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
