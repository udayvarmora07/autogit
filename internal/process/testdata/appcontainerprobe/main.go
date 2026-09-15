//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func main() {
	allowed := os.Getenv("AUTOGIT_APPCONTAINER_ALLOWED")
	denied := os.Getenv("AUTOGIT_APPCONTAINER_DENIED")
	if value, err := os.ReadFile(allowed); err != nil || string(value) != "allowed" {
		fail("allowlisted read failed: %v", err)
	}
	if _, err := os.ReadFile(denied); err == nil {
		fail("read outside the AppContainer allowlist succeeded")
	}
	writePath := filepath.Join(filepath.Dir(allowed), "write-attempt.txt")
	if err := os.WriteFile(writePath, []byte("must fail"), 0600); err == nil {
		fail("write in the read-only AppContainer directory succeeded")
	}
	fmt.Println("APPCONTAINER_PROBE_OK")
	fmt.Println("PASS")
}
