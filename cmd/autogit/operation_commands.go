package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"autogit/internal/operations"
	"autogit/internal/state"
)

func runOperationContext(ctx context.Context, args []string, dir string, out io.Writer) error {
	if len(args) == 0 {
		return cliError{"E_USAGE", "operation requires status, explain, resume, cancel, runlog, or undo"}
	}
	command := args[0]
	id := flag(args[1:], "--id")
	if command == "runlog" {
		return runLogsContext(ctx, args[1:], dir, out)
	}
	if err := validateOperationArgs(command, args[1:]); err != nil {
		return err
	}
	if id == "" {
		return cliError{"E_SCOPE", "operation --id is required"}
	}
	if command == "status" || command == "explain" || command == "resume" {
		store, err := state.OpenReadOnlyContext(ctx, filepath.Join(dir, "state.db"))
		if err != nil {
			return cliError{"E_STATE", "operation state is unavailable"}
		}
		defer store.Close()
		snapshot, err := operations.Inspect(ctx, store, id)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, os.ErrNotExist) {
			return cliError{"E_NOT_FOUND", "operation was not found"}
		}
		if err != nil {
			return cliError{"E_STATE", "operation state could not be read"}
		}
		reason := "OPERATION_STATUS"
		if command == "explain" {
			reason = "OPERATION_EXPLAIN"
		}
		if command == "resume" {
			reason = "OPERATION_RESUME"
		}
		return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "none", "reason_code": reason, "operation": snapshot})
	}
	store, err := state.OpenContext(ctx, filepath.Join(dir, "state.db"))
	if err != nil {
		return cliError{"E_STATE", "operation state is unavailable"}
	}
	defer store.Close()
	switch command {
	case "cancel":
		snapshot, err := operations.Cancel(ctx, store, id)
		if err != nil {
			return cliError{"E_CANCEL", safeMessage(err.Error())}
		}
		return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "cancel", "reason_code": "OPERATION_CANCELLED", "operation": snapshot})
	case "undo":
		repo := flag(args[1:], "--repo")
		if repo == "" {
			return cliError{"E_SCOPE", "operation undo requires --id and --repo"}
		}
		gitPath, err := trustedExecutable("git")
		if err != nil {
			return cliError{"E_PROVIDER", "git is unavailable"}
		}
		snapshot, err := operations.UndoCommitRef(ctx, store, repo, id, gitPath)
		if err != nil {
			return cliError{"E_UNDO", safeMessage(err.Error())}
		}
		return json.NewEncoder(out).Encode(map[string]any{"schema_version": "autogit.result/1", "disposition": "accepted", "action": "undo", "reason_code": "OPERATION_UNDONE", "operation": snapshot})
	}
	return cliError{"E_USAGE", "unknown operation command"}
}

func validateOperationArgs(command string, args []string) error {
	allowed := map[string]bool{"--id": true}
	if command == "undo" {
		allowed["--repo"] = true
	}
	if command != "status" && command != "explain" && command != "resume" && command != "cancel" && command != "undo" {
		return cliError{"E_USAGE", "unknown operation command"}
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if !allowed[name] || seen[name] || i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
			return cliError{"E_USAGE", "operation option requires one value and may be provided once"}
		}
		seen[name] = true
		i++
	}
	if !seen["--id"] {
		return cliError{"E_SCOPE", "operation --id is required"}
	}
	return nil
}
