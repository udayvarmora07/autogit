package adapters

import "testing"

func FuzzP303MalformedClientFieldsNeverPanic(f *testing.F) {
	adapters := make([]Adapter, 0, len(SupportedNames()))
	for _, name := range SupportedNames() {
		adapter, err := New(name)
		if err != nil {
			f.Fatalf("create %s adapter: %v", name, err)
		}
		adapters = append(adapters, adapter)
	}
	seeds := [][]byte{
		[]byte(`{"event":"idle","session_id":"s","operation_id":"o"}`),
		[]byte(`{"hook_event_name":"SessionEnd","session_id":[],"cwd":"../escape"}`),
		[]byte(`{"observation":"files.changed","files":[{"path":true}],"operation_id":"o"}`),
		[]byte(`{"event":"idle","producer_seq":999999999999999999999999}`),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		for _, adapter := range adapters {
			// Translation is intentionally total over untrusted bytes: malformed
			// values may return an error, but must never panic or invoke a side
			// effect dependency.
			_, _ = adapter.Translate(raw, TranslateOptions{ResolvedScope: map[string]string{"repo_id": matrixRepo}})
		}
	})
}
