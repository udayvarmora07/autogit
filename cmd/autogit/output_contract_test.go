package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHumanOutputIsOptInAndMutationsRemainJSONOnly(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"version", "--format", "human"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(out.String()), "{") || !strings.Contains(out.String(), "version") {
		t.Fatalf("human output=%q", out.String())
	}
	if _, _, err := normalizeOutputFormat([]string{"sync", "--format", "human"}); err == nil {
		t.Fatal("human output was allowed for mutation")
	}
}
