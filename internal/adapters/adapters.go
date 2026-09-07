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

func Manifests() []AdapterManifest {
	entries := Registry().Entries
	out := make([]AdapterManifest, 0, len(entries))
	for _, entry := range entries {
		major := ""
		if len(entry.SchemaMajors) > 0 {
			major = entry.SchemaMajors[0]
		}
		out = append(out, AdapterManifest{Name: entry.Adapter, SchemaMajor: major,
			ClientVersions: append([]string(nil), entry.ClientVersions...), EventMappings: cloneStringMap(entry.EventMappings),
			ResultExitCodes: map[string]int{"accepted": 0, "duplicate": 0, "pending": 75, "unsupported": 78, "rejected": 1},
			Contract:        entry.Contract, InstallSupported: entry.InstallSupported,
			TaskBoundaries: entry.Capabilities.TaskBoundaries, QueueState: entry.Capabilities.QueueState,
			ChangedPaths: entry.Capabilities.ChangedPaths, MonotonicSequence: entry.Capabilities.MonotonicSequence})
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
