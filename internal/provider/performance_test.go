package provider

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkFakeProviderPush(b *testing.B) {
	fake := NewFake()
	fake.Add("owner/name", "private")
	sha := "0123456789abcdef0123456789abcdef01234567"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := fake.Push(context.Background(), "owner/name", sha, fmt.Sprintf("feature/%d", i)); err != nil {
			b.Fatal(err)
		}
	}
}
