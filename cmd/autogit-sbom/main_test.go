package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildDocumentIsDeterministicAndOmitsLocalPaths(t *testing.T) {
	modules := []module{
		{Path: "autogit", Main: true},
		{Path: "example.com/z", Version: "v1.2.0"},
		{Path: "example.com/a", Version: "v1.0.0"},
	}
	opts := options{
		Name:       "autogit-v1.2.3",
		Namespace:  "https://example.invalid/sbom/v1.2.3/commit",
		RootModule: "autogit",
		Version:    "v1.2.3",
		Created:    time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
	}
	first, err := buildDocument(modules, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildDocument([]module{modules[2], modules[0], modules[1]}, opts)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("documents differ:\n%s\n%s", firstJSON, secondJSON)
	}
	if strings.Contains(string(firstJSON), "/home/") || strings.Contains(string(firstJSON), "TempDir") {
		t.Fatalf("SBOM contains a local path: %s", firstJSON)
	}
	if len(first.Packages) != 3 || first.Packages[0].Name != "autogit" || first.Packages[1].Name != "example.com/a" {
		t.Fatalf("packages=%+v", first.Packages)
	}
	if len(first.Relationships) != 3 {
		t.Fatalf("relationships=%+v", first.Relationships)
	}
}

func TestDecodeModulesRejectsMalformedOrEmptyInput(t *testing.T) {
	for _, input := range []string{"", "{}", "{\"Path\":\"\"}", "not-json"} {
		if _, err := decodeModules(strings.NewReader(input)); err == nil {
			t.Fatalf("input %q was accepted", input)
		}
	}
}

func TestEffectiveModuleDoesNotPublishLocalReplacementPath(t *testing.T) {
	got := effectiveModule(module{
		Path:    "example.com/library",
		Version: "v1.0.0",
		Replace: &module{Path: "/runner/_work/private-library", Version: ""},
	})
	if got.Path != "example.com/library" || got.Version != "" || got.Replace != nil {
		t.Fatalf("effective module=%+v", got)
	}
}

func TestRunWritesSPDXJSON(t *testing.T) {
	input := strings.NewReader(`{"Path":"autogit","Main":true}
{"Path":"example.com/library","Version":"v1.0.0"}
`)
	var output bytes.Buffer
	err := run(input, &output, "", options{
		Name:       "autogit",
		Namespace:  "https://example.invalid/sbom",
		RootModule: "autogit",
		Version:    "v1.0.0",
		Created:    time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var got document
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("SPDX JSON: %v", err)
	}
	if got.SPDXVersion != "SPDX-2.3" || got.DataLicense != "CC0-1.0" || got.CreationInfo.Created != "1970-01-01T00:00:00Z" {
		t.Fatalf("document=%+v", got)
	}
}
