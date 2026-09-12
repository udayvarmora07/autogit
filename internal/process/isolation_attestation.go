package process

import "os"

// IsolationAttestation contains only facts observed by the parent from the
// child access token. It intentionally does not include a PID, profile name,
// or user path.
type IsolationAttestation struct {
	AppContainerObserved  bool
	PackageSIDMatch       bool
	LowIntegrityObserved  bool
	NetworkDeniedObserved bool
	CapabilityCount       int
	independentlyObserved bool
}

type appContainerProcess interface {
	osProcess() *os.Process
	resume() error
	terminate() error
	wait() (*os.ProcessState, error)
	cleanup() error
	expectedPackageSID() string
}

// IndependentlyObserved distinguishes a process-package attestation from
// child-reported claims or data fabricated by a legacy runner.
func (a IsolationAttestation) IndependentlyObserved() bool {
	return a.independentlyObserved
}

// Observations returns the stable, non-identifying facts suitable for trusted
// evidence and its digest.
func (a IsolationAttestation) Observations() map[string]interface{} {
	return map[string]interface{}{
		"appcontainer_observed":   a.AppContainerObserved,
		"package_sid_match":       a.PackageSIDMatch,
		"low_integrity_observed":  a.LowIntegrityObserved,
		"network_denied_observed": a.NetworkDeniedObserved,
		"capability_count":        a.CapabilityCount,
	}
}
