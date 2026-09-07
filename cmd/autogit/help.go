package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	cl "autogit/internal/cli"
)

func writeHelp(args []string, out io.Writer) error {
	if len(args) > 1 {
		return cliError{"E_USAGE", "help accepts at most one command"}
	}
	if len(args) == 1 {
		command, ok := cl.FindCommand(args[0])
		if !ok {
			return cliError{"E_USAGE", "unknown command for help"}
		}
		_, _ = fmt.Fprintf(out, "%s\n\n%s\n\nUsage: %s\nExample: %s\n", command.Name, command.Summary, command.Usage, command.Example)
		if command.Mutates {
			_, _ = io.WriteString(out, "This command can change local or provider state; it requires explicit scoped consent.\n")
		} else {
			_, _ = io.WriteString(out, "This command is read-only by default and does not create state.\n")
		}
		return nil
	}
	_, _ = io.WriteString(out, "AutoGit — consent-bound local Git control plane\n\nCommands:\n")
	for _, command := range cl.Commands {
		_, _ = fmt.Fprintf(out, "  %-12s %s\n", command.Name, command.Summary)
	}
	_, _ = io.WriteString(out, "\nUse 'autogit help COMMAND' for usage and an example. JSON is the stable default output contract.\n")
	return nil
}

func writeHumanResult(command string, raw []byte, out io.Writer) error {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return cliError{"E_OUTPUT", "command result could not be rendered"}
	}
	_, _ = fmt.Fprintf(out, "%s\n", command)
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key == "schema_version" {
			continue
		}
		encoded, err := json.Marshal(value[key])
		if err != nil {
			return cliError{"E_OUTPUT", "command result could not be rendered"}
		}
		_, _ = fmt.Fprintf(out, "  %s: %s\n", key, encoded)
	}
	return nil
}

func runCompletion(args []string, out io.Writer) error {
	if len(args) != 1 || (args[0] != "bash" && args[0] != "zsh" && args[0] != "fish") {
		return cliError{"E_USAGE", "completion requires bash, zsh, or fish"}
	}
	names := make([]string, 0, len(cl.Commands)+3)
	for _, command := range cl.Commands {
		names = append(names, command.Name)
	}
	names = append(names, "help", "completion", "hook")
	switch args[0] {
	case "bash":
		_, _ = fmt.Fprintf(out, "_autogit_complete() { COMPREPLY=( $(compgen -W '%s' -- \"${COMP_WORDS[COMP_CWORD]}\") ); }; complete -F _autogit_complete autogit\n", strings.Join(names, " "))
	case "zsh":
		_, _ = fmt.Fprintf(out, "#compdef autogit\n_arguments '1:command:(%s)'\n", strings.Join(names, " "))
	case "fish":
		for _, name := range names {
			_, _ = fmt.Fprintf(out, "complete -c autogit -f -n '__fish_use_subcommand' -a %s\n", name)
		}
	}
	return nil
}
