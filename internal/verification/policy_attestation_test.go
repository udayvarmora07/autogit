package verification

import "testing"

func TestTrustedEvidenceRequiresObservedAppContainerFacts(t *testing.T) {
	evidence := TrustedEvidence{
		Passed:             true,
		IsolationTier:      TierFilesystemNetworkIsolated,
		IsolationPrimitive: "appcontainer+job-object",
		ExecutableBinding:  "digest-rechecked-path",
		CandidateDigest:    testDigest('a'),
		BaseDigest:         testDigest('b'),
		PolicyDigest:       testDigest('c'),
		GuardDigest:        testDigest('d'),
		VerifierSetDigest:  testDigest('e'),
	}
	if evidence.ValidForTrusted(testDigest('a'), testDigest('b'), testDigest('c'), testDigest('d'), testDigest('e')) {
		t.Fatal("AppContainer evidence without parent observations was accepted")
	}
	evidence.IsolationObservations = map[string]interface{}{
		"appcontainer_observed":   true,
		"package_sid_match":       true,
		"low_integrity_observed":  true,
		"network_denied_observed": true,
		"capability_count":        0,
	}
	if !evidence.ValidForTrusted(testDigest('a'), testDigest('b'), testDigest('c'), testDigest('d'), testDigest('e')) {
		t.Fatal("complete AppContainer parent observations were rejected")
	}
}
