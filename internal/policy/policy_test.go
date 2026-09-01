package policy

import "testing"

func TestEffectivePolicyProjectOverridesOnlyExplicitFields(t *testing.T) {
	got := Merge(Policy{Tracking: "local", Visibility: "private", Workflow: "safe"}, Policy{Visibility: "public"})
	if got.Tracking != "local" || got.Visibility != "public" || got.Workflow != "safe" {
		t.Fatalf("unexpected policy: %+v", got)
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
	if err := Validate(Policy{Visibility: "world"}); err == nil {
		t.Fatal("unknown visibility accepted")
	}
}
