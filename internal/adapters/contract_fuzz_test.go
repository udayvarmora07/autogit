package adapters

import "testing"

func FuzzP303MalformedClientFieldsNeverPanic(f *testing.F) {
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
		for _, name := range SupportedNames() {
			a, err := New(name)
			if err != nil {
				t.Fatal(err)
			}
			// Translation is intentionally total over untrusted bytes: malformed
			// values may return an error, but must never panic or invoke a side
			// effect dependency.
			_, _ = a.Translate(raw, TranslateOptions{ResolvedScope: map[string]string{"repo_id": matrixRepo}})
		}
	})
}
