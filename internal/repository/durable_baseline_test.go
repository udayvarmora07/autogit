package repository

import (
	"strings"
	"testing"
)

func TestDurableBaselineEvidenceOmitsPathsAndSourceBytes(t *testing.T) {
	baseline := Baseline{
		PathsDigest: DigestPaths([]string{"secret/project.txt"}),
		Paths:       []string{"secret/project.txt"},
		Files: map[string]FileObservation{
			"secret/project.txt": {Content: []byte("private source"), Mode: 0644, Present: true},
		},
	}
	raw, err := EncodeDurableBaseline(baseline, []byte("identity-key"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "secret/project.txt") || strings.Contains(raw, "private source") {
		t.Fatalf("durable evidence leaked sensitive content: %s", raw)
	}
	decoded, err := DecodeDurableBaseline(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.PathsDigest != baseline.PathsDigest || len(decoded.Files) != 1 {
		t.Fatalf("decoded evidence=%+v", decoded)
	}
}

func TestDurableBaselineEvidencePathIDsAreKeyBound(t *testing.T) {
	baseline := Baseline{Paths: []string{"file.txt"}, PathsDigest: DigestPaths([]string{"file.txt"}), Files: map[string]FileObservation{"file.txt": {Content: []byte("same"), Present: true}}}
	first, err := EncodeDurableBaseline(baseline, []byte("first-key"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncodeDurableBaseline(baseline, []byte("second-key"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different identity keys produced identical baseline evidence")
	}
}

func TestDurableBaselineEvidenceRejectsMalformedOrOversizedInput(t *testing.T) {
	if _, err := DecodeDurableBaseline(`{"version":1,"paths_digest":"sha256:bad","files":[]}`); err == nil {
		t.Fatal("malformed durable evidence accepted")
	}
	if _, err := EncodeDurableBaseline(Baseline{Paths: []string{"file.txt"}, PathsDigest: DigestPaths([]string{"file.txt"}), Files: map[string]FileObservation{"file.txt": {Content: []byte("x"), Present: true}}}, nil); err == nil {
		t.Fatal("missing identity key accepted")
	}
}
