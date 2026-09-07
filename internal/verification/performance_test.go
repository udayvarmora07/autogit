package verification

import (
	"context"
	"os"
	"testing"
)

type benchmarkRunner struct{}

func (benchmarkRunner) Run(context.Context, string, map[string]string, ...string) (Result, error) {
	return Result{ExitCode: 0}, nil
}

func BenchmarkTrustedVerificationBoundary(b *testing.B) {
	executable, err := os.Executable()
	if err != nil {
		b.Fatal(err)
	}
	registry, err := NewVerifierRegistry([]TrustedVerifierSpec{{Name: "benchmark", Version: "1", Argv: []string{executable}, Applicable: true}})
	if err != nil {
		b.Fatal(err)
	}
	request := TrustedRequest{CandidateDigest: testDigest('a'), BaseDigest: testDigest('b'), PolicyDigest: testDigest('c'), GuardDigest: testDigest('d'), Dir: b.TempDir()}
	policy := VerificationPolicy{Visibility: "private"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if result, err := registry.Verify(context.Background(), policy, request, benchmarkRunner{}); err != nil || !result.Passed {
			b.Fatalf("verification result=%+v err=%v", result, err)
		}
	}
}
