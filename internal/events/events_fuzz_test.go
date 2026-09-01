package events

import "testing"

func FuzzDecodeNeverPanics(f *testing.F) {
	f.Add([]byte(validEvent))
	f.Add([]byte(`{"schema_version":"autogit.event/2"}`))
	f.Add([]byte(`{"payload":{}}`))
	f.Fuzz(func(t *testing.T, input []byte) { _, _ = Decode(input, 64<<10) })
}
