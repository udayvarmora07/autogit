package autogit

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var requirementIDPattern = regexp.MustCompile(`\b((?:FR|NFR)-[A-Z]+-[0-9]{3})\b`)
var traceabilityRangePattern = regexp.MustCompile(`(?:FR|NFR)-[A-Z]+-[0-9]{3}(?:\.\.[0-9]{3})?`)

func TestMustLevelRequirementsHaveTraceabilityRows(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository root")
	}
	root := filepath.Dir(source)
	requirements, err := os.ReadFile(filepath.Join(root, "docs", "product-requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	strategy, err := os.ReadFile(filepath.Join(root, "docs", "test-strategy.md"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, match := range requirementIDPattern.FindAllStringSubmatch(string(requirements), -1) {
		if seen[match[1]] {
			t.Fatalf("duplicate product requirement ID %q", match[1])
		}
		seen[match[1]] = true
	}
	if len(seen) == 0 {
		t.Fatal("product requirements contain no requirement IDs")
	}
	coverage := traceabilityRangePattern.FindAllString(string(strategy), -1)
	for id := range seen {
		if !requirementCovered(id, coverage) {
			t.Errorf("requirement %s has no traceability row", id)
		}
	}
}

func requirementCovered(id string, rows []string) bool {
	last := strings.LastIndexByte(id, '-')
	if last < 0 {
		return false
	}
	prefix, rawNumber := id[:last], id[last+1:]
	number, err := strconv.Atoi(rawNumber)
	if err != nil {
		return false
	}
	for _, row := range rows {
		rowLast := strings.LastIndexByte(row, '-')
		if rowLast < 0 || row[:rowLast] != prefix {
			continue
		}
		rangeParts := strings.Split(row[rowLast+1:], "..")
		start, startErr := strconv.Atoi(rangeParts[0])
		if startErr != nil {
			continue
		}
		end := start
		if len(rangeParts) == 2 {
			end, startErr = strconv.Atoi(rangeParts[1])
			if startErr != nil {
				continue
			}
		}
		if number >= start && number <= end {
			return true
		}
	}
	return false
}
