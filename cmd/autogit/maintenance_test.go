package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	sharedDB "autogit/internal/db"
)

func TestMaintenanceCommandsExposeSafeStateOperations(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("AUTOGIT_STATE_DIR", stateRoot)
	statePath := filepath.Join(stateRoot, "state.db")
	database, err := sharedDB.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO audit(id,reason_code,metadata,at) VALUES('maintenance-test','TEST','{}',1)`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	var integrity bytes.Buffer
	if err := run([]string{"integrity"}, strings.NewReader(""), &integrity); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(integrity.String(), `"integrity_ok":true`) {
		t.Fatalf("integrity output=%s", integrity.String())
	}

	backupPath := filepath.Join(t.TempDir(), "state-backup.db")
	var backup bytes.Buffer
	if err := run([]string{"backup", "--output", backupPath}, strings.NewReader(""), &backup); err != nil {
		t.Fatal(err)
	}
	var backupJSON map[string]any
	if err := json.Unmarshal(backup.Bytes(), &backupJSON); err != nil || backupJSON["result"] == nil {
		t.Fatalf("backup output=%s err=%v", backup.String(), err)
	}

	var export bytes.Buffer
	if err := run([]string{"export"}, strings.NewReader(""), &export); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(export.String(), "maintenance-test") {
		t.Fatalf("export leaked audit identity: %s", export.String())
	}
	if err := run([]string{"restore", "--input", backupPath}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}
