package policy

import "testing"

func TestEffectivePolicyProjectOverridesOnlyExplicitFields(t *testing.T) {
	got := Merge(Policy{Tracking: "local", Visibility: "private", Workflow: "safe"}, Policy{Visibility: "public"})
	if got.Tracking != "local" || got.Visibility != "public" || got.Workflow != "safe" {
		t.Fatalf("unexpected policy: %+v", got)
	}
}

func TestMergePreservesExplicitTrustedCompletionProfile(t *testing.T) {
	got := Merge(Policy{Tracking: "local"}, Policy{AutoComplete: true, VerifierConfig: "verifiers/project.json"})
	if !got.AutoComplete || got.VerifierConfig != "verifiers/project.json" {
		t.Fatalf("completion profile was not merged: %+v", got)
	}
}

func TestValidateRequiresTrustedVerifierConfigForAutomaticCompletion(t *testing.T) {
	if err := Validate(Policy{Tracking: "local", AutoComplete: true}); err == nil {
		t.Fatal("automatic completion without a verifier config was accepted")
	}
}

func TestLocalOnlyForbidsProviderRegardlessOfRemote(t *testing.T) {
	p := Policy{Tracking: "local", LocalOnly: true, Visibility: "public"}
	if p.ProviderAllowed() {
		t.Fatal("local-only policy allowed provider")
	}
}

func TestPublicRequiresExplicitConsent(t *testing.T) {
	if (Policy{Tracking: "yes", Visibility: "public"}).CanPublishPublic() {
		t.Fatal("public publication allowed without explicit consent")
	}
	if !(Policy{Tracking: "yes", Visibility: "public", PublicConsent: true}).CanPublishPublic() {
		t.Fatal("explicit public consent was not accepted")
	}
}

func TestValidateRejectsUnknownPolicyValues(t *testing.T) {
	if err := Validate(Policy{Tracking: "maybe"}); err == nil {
		t.Fatal("unknown tracking accepted")
	}
	if err := Validate(Policy{Tracking: "public"}); err == nil {
		t.Fatal("deprecated public tracking value accepted")
	}
	if err := Validate(Policy{Visibility: "world"}); err == nil {
		t.Fatal("unknown visibility accepted")
	}
}

func TestTrackingPredicatesHaveExplicitThreeStateSemantics(t *testing.T) {
	tests := []struct {
		name       string
		tracking   string
		enabled    bool
		providerOK bool
		publicOK   bool
	}{
		{name: "unset", tracking: "", enabled: false, providerOK: false, publicOK: false},
		{name: "yes", tracking: "yes", enabled: true, providerOK: true, publicOK: true},
		{name: "local", tracking: "local", enabled: true, providerOK: false, publicOK: false},
		{name: "no", tracking: "no", enabled: false, providerOK: false, publicOK: false},
		{name: "deprecated-public", tracking: "public", enabled: false, providerOK: false, publicOK: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Policy{Tracking: test.tracking, Provider: "github", Visibility: "public", PublicConsent: true}
			if got := p.TrackingEnabled(); got != test.enabled {
				t.Fatalf("TrackingEnabled()=%v, want %v", got, test.enabled)
			}
			if got := p.ProviderAllowed(); got != test.providerOK {
				t.Fatalf("ProviderAllowed()=%v, want %v", got, test.providerOK)
			}
			if got := p.CanPublishPublic(); got != test.publicOK {
				t.Fatalf("CanPublishPublic()=%v, want %v", got, test.publicOK)
			}
		})
	}
}
