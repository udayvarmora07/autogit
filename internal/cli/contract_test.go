package cli

import "testing"

func TestCommandContractHasReadOnlyDiagnosticsAndExplicitMutations(t *testing.T) {
	for _, name := range []string{"version", "doctor", "status", "plan", "logs", "mcp", "operation"} {
		command, ok := FindCommand(name)
		if !ok || command.Summary == "" || command.Usage == "" || command.Example == "" {
			t.Fatalf("incomplete command contract for %q: %+v", name, command)
		}
	}
	status, _ := FindCommand("status")
	publish, _ := FindCommand("publish")
	if status.Mutates || !publish.Mutates {
		t.Fatal("mutation classification is incorrect")
	}
	if ExitCode("E_USAGE") != 2 || ExitCode("E_INTERNAL") != 1 {
		t.Fatal("unstable exit code classification")
	}
}
