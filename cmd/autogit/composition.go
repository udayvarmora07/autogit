package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	cl "autogit/internal/cli"
	"autogit/internal/security"
	"autogit/internal/telemetry"
)

// cliError is the application-level error contract. Parsers produce stable
// codes; the composition root owns redaction, formatting, and exit mapping.
type cliError struct{ Code, Message string }

const defaultOperationTimeout = 5 * time.Minute

var (
	buildVersion       = "dev"
	buildCommit        = "unknown"
	buildDate          = "unknown"
	buildCompatibility = "autogit.compatibility/1"
)

func (e cliError) Error() string { return e.Code + ": " + e.Message }

func stateDir() (string, error) {
	if p := os.Getenv("AUTOGIT_STATE_DIR"); p != "" {
		return p, nil
	}
	p, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, "autogit"), nil
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		code := "E_INTERNAL"
		var ce cliError
		if errors.As(err, &ce) {
			code = ce.Code
		}
		_ = json.NewEncoder(os.Stderr).Encode(cl.ErrorEnvelope{Error: cl.Error{Code: code, Message: safeMessage(err.Error())}})
		os.Exit(cl.ExitCode(code))
	}
}

func safeMessage(s string) string {
	s = security.Redact(strings.ReplaceAll(s, "\n", " "))
	parts := strings.Fields(s)
	for i, part := range parts {
		clean := strings.Trim(part, "()[]{}<>,;\"'")
		if strings.HasPrefix(clean, "/") || strings.Contains(clean, `:\`) || strings.HasPrefix(clean, `\\`) {
			parts[i] = strings.Replace(part, clean, "<path>", 1)
		}
	}
	s = strings.Join(parts, " ")
	if len(s) > 256 {
		s = s[:256]
	}
	return s
}

func writeVersion(out io.Writer) error {
	return json.NewEncoder(out).Encode(map[string]string{
		"schema_version": "autogit.result/1", "version": buildVersion, "commit": buildCommit,
		"build_date": buildDate, "compatibility": buildCompatibility,
	})
}

func run(args []string, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
	defer cancel()
	started := time.Now()
	normalized, format, formatErr := normalizeOutputFormat(args)
	if formatErr != nil {
		recordCommandTelemetry(args, started, formatErr)
		return formatErr
	}
	var commandOut io.Writer = out
	var human bytes.Buffer
	if format == "human" {
		commandOut = &human
	}
	err := runWithContext(ctx, normalized, in, commandOut)
	if err == nil && format == "human" {
		err = writeHumanResult(normalized[0], human.Bytes(), out)
	}
	recordCommandTelemetry(args, started, err)
	return err
}

func normalizeOutputFormat(args []string) ([]string, string, error) {
	if len(args) == 0 {
		return args, "json", nil
	}
	format := "json"
	result := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] != "--format" {
			result = append(result, args[i])
			continue
		}
		if i+1 >= len(args) || (args[i+1] != "json" && args[i+1] != "human") || format != "json" {
			return nil, "", cliError{"E_USAGE", "--format must be json or human and may be provided once"}
		}
		format = args[i+1]
		i++
	}
	if format == "human" {
		if len(result) == 0 {
			return nil, "", cliError{"E_USAGE", "--format must follow a command"}
		}
		switch result[0] {
		case "status", "plan", "doctor", "logs", "config", "integrity", "export", "operation", "version":
		default:
			return nil, "", cliError{"E_USAGE", "human output is available for read-only commands only"}
		}
	}
	return result, format, nil
}

func recordCommandTelemetry(args []string, started time.Time, commandErr error) {
	if os.Getenv("AUTOGIT_TELEMETRY") != "local" || len(args) == 0 {
		return
	}
	dir, err := stateDir()
	if err != nil {
		return
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return
	}
	recorder, err := telemetry.New(filepath.Join(dir, "telemetry.jsonl"), true)
	if err != nil {
		return
	}
	outcome := "accepted"
	errorCode := ""
	if commandErr != nil {
		outcome = "rejected"
		var ce cliError
		if errors.As(commandErr, &ce) {
			errorCode = ce.Code
		}
	}
	_ = recorder.Record(context.Background(), telemetry.Event{Kind: "command", Name: args[0], Outcome: outcome, ErrorCode: errorCode, DurationMS: time.Since(started).Milliseconds()})
}
