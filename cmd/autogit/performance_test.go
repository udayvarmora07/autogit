package main

import (
	"bytes"
	"testing"
)

func BenchmarkCLIStartupVersion(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := run([]string{"version"}, bytes.NewReader(nil), &out); err != nil {
			b.Fatal(err)
		}
	}
}
