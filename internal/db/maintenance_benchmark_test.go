package db

import (
	"context"
	"path/filepath"
	"testing"
)

func BenchmarkStateInspect(b *testing.B) {
	path := filepath.Join(b.TempDir(), "state.db")
	database, err := Open(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := database.Close(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Inspect(context.Background(), path); err != nil {
			b.Fatal(err)
		}
	}
}
