package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"autogit/internal/compatibility"
)

func main() {
	manifestPath := flag.String("manifest", "docs/compatibility-manifest.json", "compatibility manifest path")
	warningDays := flag.Int("warning-days", 60, "days before review expiry to flag")
	issueBody := flag.String("issue-body", "", "write a redacted issue body when review is due")
	flag.Parse()
	if *warningDays < 0 {
		fail("warning-days cannot be negative")
	}
	manifest, err := compatibility.Load(*manifestPath)
	if err != nil {
		fail(err.Error())
	}
	report, err := compatibility.Validate(manifest, time.Now().UTC(), time.Duration(*warningDays)*24*time.Hour)
	if err != nil {
		fail(err.Error())
	}
	encoded, _ := json.Marshal(struct {
		SchemaVersion string                       `json:"schema_version"`
		Due           bool                         `json:"due"`
		Expired       bool                         `json:"expired"`
		Windows       []compatibility.WindowStatus `json:"windows"`
	}{"autogit.compatibility-report/1", report.Due(), report.Expired(), report.Windows})
	fmt.Println(string(encoded))
	if report.Due() && *issueBody != "" {
		var body strings.Builder
		body.WriteString("# Compatibility window expiry review\n\n")
		body.WriteString("Review the advertised compatibility contract before the next release.\n\n")
		for _, window := range report.Windows {
			if window.Due {
				fmt.Fprintf(&body, "- `%s`: review by `%s` (expired=%t)\n", window.Name, window.ReviewBy, window.Expired)
			}
		}
		if err := os.WriteFile(*issueBody, []byte(body.String()), 0600); err != nil {
			fail(err.Error())
		}
	}
	if report.Expired() {
		os.Exit(10)
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
