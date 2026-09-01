package adapters

import "testing"

func TestInitialAdapterManifestCoversAllSupportedClients(t *testing.T) {
	want := []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode", "commandcode"}
	got := Manifests()
	if len(got) != len(want) {
		t.Fatalf("manifest count=%d", len(got))
	}
	for i, name := range want {
		if got[i].Name != name || got[i].SchemaMajor != "autogit.event/1" {
			t.Fatalf("manifest[%d]=%+v", i, got[i])
		}
	}
}

func TestUnknownAdapterIsRejectedWithoutMutation(t *testing.T) {
	if _, err := Manifest("unknown"); err == nil {
		t.Fatal("unknown adapter accepted")
	}
}
