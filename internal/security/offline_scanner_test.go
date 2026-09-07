package security

import (
	"context"
	"strings"
	"testing"
)

func TestPinnedOfflineScannerReportsVersionCoverageAndRedactedFindings(t *testing.T) {
	scanner := NewPinnedOfflineScanner()
	report := scanner.Scan(context.Background(), CandidateSnapshot{Files: []CandidateFile{
		{Path: "config.env", Content: []byte("GH_TOKEN=secret-value")},
		{Path: "safe.txt", Content: []byte("safe")},
	}})
	if report.Scanner != OfflineScannerVersion || report.Coverage.FilesPresented != 2 || report.Coverage.FilesScanned != 2 || report.Coverage.TotalBytes == 0 || report.Truncated {
		t.Fatalf("unexpected scanner report: %+v", report)
	}
	if !report.Result.Blocked || report.EvidenceDigest == "" || len(report.RedactedFingerprints) != 1 || report.ProviderGuidance == "" {
		t.Fatalf("scanner did not produce bound evidence: %+v", report)
	}
	if strings.Contains(report.EvidenceDigest, "secret-value") || strings.Contains(report.RedactedSummary, "secret-value") {
		t.Fatalf("secret leaked in scanner evidence: %+v", report)
	}
}

func TestPinnedOfflineScannerMarksLimitState(t *testing.T) {
	scanner := NewPinnedOfflineScanner()
	report := scanner.Scan(context.Background(), CandidateSnapshot{Files: []CandidateFile{
		{Path: "one.txt", Content: []byte("1234")},
		{Path: "two.txt", Content: []byte("5678")},
	}}, ScanOptions{Limits: Limits{MaxFiles: 1}})
	if !report.Truncated || !report.Result.Blocked || report.Coverage.LimitReason == "" {
		t.Fatalf("limit state not reported: %+v", report)
	}
}
