// Package cli owns the stable, machine-readable command contract. Keeping the
// wire shape here lets command parsing, presenters, the MCP adapter, and
// documentation share one vocabulary without importing the command package.
package cli

const ResultSchemaVersion = "autogit.result/1"

type ErrorEnvelope struct {
	Error Error `json:"error"`
}

type Error struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

type CommandSpec struct {
	Name    string
	Summary string
	Usage   string
	Mutates bool
	Example string
}

var Commands = []CommandSpec{
	{Name: "version", Summary: "Print build and compatibility identity", Usage: "autogit version", Example: "autogit version"},
	{Name: "doctor", Summary: "Report offline capability and state health", Usage: "autogit doctor", Example: "autogit doctor"},
	{Name: "status", Summary: "Read lifecycle and repository status", Usage: "autogit status --repo DIR", Example: "autogit status --repo ."},
	{Name: "plan", Summary: "Preview the observed candidate and consent checks", Usage: "autogit plan --repo DIR", Example: "autogit plan --repo ."},
	{Name: "logs", Summary: "Read bounded redacted audit logs", Usage: "autogit logs --repo DIR [--limit N]", Example: "autogit logs --repo . --limit 20"},
	{Name: "operation", Summary: "Inspect, explain, resume, cancel, runlog, or undo one owned operation", Usage: "autogit operation status|explain|resume|cancel|runlog|undo", Example: "autogit operation status --id OPERATION_ID"},
	{Name: "config", Summary: "Explain or safely migrate owned configuration", Usage: "autogit config explain [--verifiers FILE]", Example: "autogit config explain --verifiers verifiers.json"},
	{Name: "mcp", Summary: "Serve the optional read-only MCP boundary", Usage: "autogit mcp serve", Example: "autogit mcp serve"},
	{Name: "init", Summary: "Initialize explicit local or private tracking consent", Usage: "autogit init --repo DIR --local", Mutates: true, Example: "autogit init --repo . --local"},
	{Name: "sync", Summary: "Create a verified AutoGit-owned local commit", Usage: "autogit sync --complete ...", Mutates: true, Example: "autogit sync --complete --all-owned --id ID --repo DIR --session ID --client codex"},
	{Name: "verify", Summary: "Run trusted verification without committing", Usage: "autogit verify ...", Mutates: true, Example: "autogit verify --all-owned ..."},
	{Name: "publish", Summary: "Publish one exact, already-verified commit", Usage: "autogit publish ...", Mutates: true, Example: "autogit publish --id ID ... --mode private"},
	{Name: "remote", Summary: "Create and bind an explicitly consented remote", Usage: "autogit remote create ...", Mutates: true, Example: "autogit remote create --id ID --repo DIR --alias origin --owner O --name N"},
	{Name: "backup", Summary: "Create a validated standalone SQLite backup", Usage: "autogit backup --output FILE", Mutates: true, Example: "autogit backup --output state-backup.db"},
	{Name: "restore", Summary: "Restore a validated SQLite backup", Usage: "autogit restore --input FILE", Mutates: true, Example: "autogit restore --input state-backup.db"},
	{Name: "integrity", Summary: "Inspect SQLite integrity without repair", Usage: "autogit integrity", Example: "autogit integrity"},
	{Name: "repair", Summary: "Run explicit non-destructive SQLite maintenance", Usage: "autogit repair", Mutates: true, Example: "autogit repair"},
	{Name: "retain", Summary: "Prune bounded non-active records", Usage: "autogit retain [options]", Mutates: true, Example: "autogit retain --audit-age-hours 720"},
	{Name: "export", Summary: "Export structural redacted state counts", Usage: "autogit export", Example: "autogit export"},
	{Name: "install", Summary: "Install an owned client adapter hook", Usage: "autogit install ...", Mutates: true, Example: "autogit install --adapter codex --path config.json --root ."},
	{Name: "uninstall", Summary: "Remove only an AutoGit-owned adapter hook", Usage: "autogit uninstall ...", Mutates: true, Example: "autogit uninstall --adapter codex --path config.json --root ."},
}

func FindCommand(name string) (CommandSpec, bool) {
	for _, command := range Commands {
		if command.Name == name {
			return command, true
		}
	}
	return CommandSpec{}, false
}

func ExitCode(code string) int {
	switch code {
	case "E_USAGE", "E_SCHEMA", "E_SCOPE", "E_CONSENT", "E_PROVIDER", "E_UNSUPPORTED":
		return 2
	case "E_TIMEOUT", "E_PUSH", "E_REMOTE", "E_RETRY":
		return 3
	default:
		return 1
	}
}
