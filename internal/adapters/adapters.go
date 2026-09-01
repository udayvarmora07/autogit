package adapters

import "fmt"

type AdapterManifest struct {
	Name              string
	SchemaMajor       string
	ClientVersions    []string
	EventMappings     map[string]string
	ResultExitCodes   map[string]int
	Contract          string
	InstallSupported  bool
	TaskBoundaries    string
	QueueState        string
	ChangedPaths      string
	MonotonicSequence bool
}

var manifests = []AdapterManifest{
	{Name: "codex", SchemaMajor: "autogit.event/1", ClientVersions: []string{"unknown", "0.x", "1.x", "2.x"}, Contract: "official-hook", InstallSupported: true, TaskBoundaries: "native", QueueState: "unknown", ChangedPaths: "reported", MonotonicSequence: true},
	{Name: "claude-code", SchemaMajor: "autogit.event/1", ClientVersions: []string{"unknown", "1.x", "2.x"}, Contract: "official-hook", InstallSupported: true, TaskBoundaries: "native", QueueState: "unknown", ChangedPaths: "reported", MonotonicSequence: true},
	{Name: "cursor", SchemaMajor: "autogit.event/1", ClientVersions: []string{"observation"}, Contract: "synthetic-observation", TaskBoundaries: "synthetic", QueueState: "none", ChangedPaths: "none"},
	{Name: "gemini-cli", SchemaMajor: "autogit.event/1", ClientVersions: []string{"unknown", "0.x", "1.x", "2.x"}, Contract: "official-hook", InstallSupported: true, TaskBoundaries: "native", QueueState: "unknown", ChangedPaths: "reported", MonotonicSequence: true},
	{Name: "opencode", SchemaMajor: "autogit.event/1", ClientVersions: []string{"observation"}, Contract: "synthetic-observation", TaskBoundaries: "synthetic", QueueState: "none", ChangedPaths: "derived"},
	{Name: "commandcode", SchemaMajor: "autogit.event/1", ClientVersions: []string{"observation"}, Contract: "synthetic-observation", TaskBoundaries: "synthetic", QueueState: "unknown", ChangedPaths: "derived"},
}

func Manifests() []AdapterManifest {
	out := make([]AdapterManifest, len(manifests))
	copy(out, manifests)
	for i := range out {
		contract := clientContracts[out[i].Name]
		out[i].ClientVersions = append([]string(nil), contract.versions...)
		out[i].EventMappings = cloneStringMap(contract.mapping)
		out[i].ResultExitCodes = map[string]int{"accepted": 0, "duplicate": 0, "pending": 75, "unsupported": 78, "rejected": 1}
		out[i].InstallSupported = contract.install
		out[i].Contract = contract.contract
	}
	return out
}
func Manifest(name string) (AdapterManifest, error) {
	for _, m := range Manifests() {
		if m.Name == name {
			return m, nil
		}
	}
	return AdapterManifest{}, fmt.Errorf("unknown adapter %q", name)
}
