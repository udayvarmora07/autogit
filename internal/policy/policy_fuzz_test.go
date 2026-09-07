package policy

import "testing"

func FuzzPolicyValidationAndMergeNeverPanics(f *testing.F) {
	f.Add("local", "private", "", "", "", "safe", true, false, false, 1)
	f.Add("yes", "public", "github", "owner", "owner/repo", "fast", false, true, true, 2)
	f.Fuzz(func(t *testing.T, tracking, visibility, provider, owner, destination, workflow string, localOnly, publicConsent, autoComplete bool, version int) {
		base := Policy{Tracking: tracking, Visibility: visibility, Provider: provider, Owner: owner, Destination: destination, Workflow: workflow, LocalOnly: localOnly, PublicConsent: publicConsent, AutoComplete: autoComplete, Version: version}
		_ = Validate(base)
		_ = Validate(Merge(Policy{}, base))
	})
}
