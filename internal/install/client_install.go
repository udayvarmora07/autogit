package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"autogit/internal/adapters"
)

// ClientInstallation is the registry consumed by the CLI. Supported entries
// correspond to documented command-hook configuration schemas; an
// unsupported entry is intentionally fail-closed until its client exposes a
// stable hook contract.
type ClientInstallation struct {
	Adapter           string
	Supported         bool
	ConfigFormat      Format
	ConfigShape       string
	HookEvent         string
	UnsupportedReason string
}

type ClientInstallPlan struct {
	Installation ClientInstallation
	Plan         InstallPlan
}

var ErrUnsupported = errors.New("client hook installation is unsupported")

func ClientInstallations() []ClientInstallation {
	registry := adapters.Registry()
	out := make([]ClientInstallation, 0, len(registry.Entries))
	for _, entry := range registry.Entries {
		var format Format
		if entry.InstallSupported {
			format = Format(entry.Config.Format)
		}
		out = append(out, ClientInstallation{Adapter: entry.Adapter, Supported: entry.InstallSupported,
			ConfigFormat: format, ConfigShape: entry.Config.Shape, HookEvent: entry.Config.HookEvent,
			UnsupportedReason: entry.UnsupportedReason})
	}
	return out
}

func ClientInstallationFor(adapter string) (ClientInstallation, error) {
	for _, entry := range ClientInstallations() {
		if entry.Adapter == adapter {
			return entry, nil
		}
	}
	return ClientInstallation{}, fmt.Errorf("%w: unknown client %q", ErrUnsupported, adapter)
}

// PlanClient computes a client-specific hook plan. projectRoot is variadic
// only to preserve source compatibility with early callers; exactly one root
// is required for a safe command plan.
func PlanClient(entry ClientInstallation, path string, roots []string, projectRoots ...string) (ClientInstallPlan, error) {
	if !entry.Supported {
		return ClientInstallPlan{}, fmt.Errorf("%w: %s", ErrUnsupported, entry.UnsupportedReason)
	}
	if entry.ConfigFormat != FormatJSON || entry.HookEvent == "" {
		return ClientInstallPlan{}, ErrUnsupported
	}
	if len(projectRoots) != 1 {
		return ClientInstallPlan{}, fmt.Errorf("%w: exactly one project root is required", ErrScope)
	}
	projectRoot, err := canonicalProjectRoot(projectRoots[0])
	if err != nil {
		return ClientInstallPlan{}, err
	}
	clean, err := checkedPath(path, roots)
	if err != nil {
		return ClientInstallPlan{}, err
	}
	p := InstallPlan{Spec: ConfigSpec{Adapter: entry.Adapter, Path: clean, Format: FormatJSON}, Path: clean, Mode: 0600, resolvedDir: resolvedInstallDir(filepath.Dir(clean))}
	info, statErr := os.Lstat(clean)
	if statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ClientInstallPlan{}, ErrScope
		}
		p.Exists = true
		p.Mode = info.Mode()
		p.Original, err = os.ReadFile(clean) // #nosec G304 -- clean is constrained to the caller-approved config root.
		if err != nil {
			return ClientInstallPlan{}, err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return ClientInstallPlan{}, statErr
	}
	if p.Exists && len(p.Original) > 4<<20 {
		return ClientInstallPlan{}, ErrFormat
	}
	obj := map[string]any{}
	if p.Exists {
		if err := decodeJSON(p.Original, &obj); err != nil {
			return ClientInstallPlan{}, fmt.Errorf("%w: %v", ErrFormat, err)
		}
	}
	command, err := hookCommand(entry, projectRoot)
	if err != nil {
		return ClientInstallPlan{}, err
	}
	if err := addClientHook(obj, entry, command); err != nil {
		return ClientInstallPlan{}, err
	}
	p.Desired, err = json.Marshal(obj)
	if err != nil {
		return ClientInstallPlan{}, err
	}
	p.Changed = !bytes.Equal(p.Original, p.Desired) || (p.Exists && p.Mode.Perm() != 0600)
	return ClientInstallPlan{Installation: entry, Plan: p}, nil
}

func ApplyClient(p ClientInstallPlan) error {
	if !p.Installation.Supported {
		return ErrUnsupported
	}
	return ApplyWithProvenance(p.Plan)
}

func canonicalProjectRoot(root string) (string, error) {
	if root == "" || !filepath.IsAbs(root) || strings.ContainsAny(root, "\x00\r\n") {
		return "", ErrScope
	}
	clean := filepath.Clean(root)
	info, err := os.Lstat(clean)
	if err != nil {
		return "", fmt.Errorf("%w: project root is unavailable", ErrScope)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("%w: project root must be a real directory", ErrScope)
	}
	return clean, nil
}

func hookCommand(entry ClientInstallation, root string) (string, error) {
	event := "session.ended"
	if entry.Adapter == "claude-code" {
		event = "task.completed"
	}
	quotedRoot, err := shellQuote(root)
	if err != nil {
		return "", err
	}
	return "autogit hook --adapter " + entry.Adapter + " --event " + event + " --root " + quotedRoot, nil
}

func shellQuote(value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("%w: invalid project root", ErrScope)
	}
	if runtime.GOOS == "windows" {
		// cmd.exe expands these metacharacters even in otherwise ordinary
		// command strings. Rejecting them is safer than guessing a quoting
		// dialect for a client-owned command runner.
		if strings.ContainsAny(value, "&|<>^%!\"") {
			return "", fmt.Errorf("%w: project root contains shell metacharacter", ErrScope)
		}
		return `"` + value + `"`, nil
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'", nil
}

func UninstallClient(entry ClientInstallation, path string, roots []string, projectRoots ...string) error {
	if !entry.Supported {
		return fmt.Errorf("%w: %s", ErrUnsupported, entry.UnsupportedReason)
	}
	if len(projectRoots) != 1 {
		return fmt.Errorf("%w: exactly one project root is required", ErrScope)
	}
	projectRoot, err := canonicalProjectRoot(projectRoots[0])
	if err != nil {
		return err
	}
	clean, err := checkedPath(path, roots)
	if err != nil {
		return err
	}
	info, err := os.Lstat(clean)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrScope
	}
	original, err := os.ReadFile(clean) // #nosec G304 -- clean is constrained to the caller-approved config root.
	if err != nil {
		return err
	}
	command, err := hookCommand(entry, projectRoot)
	if err != nil {
		return err
	}
	obj := map[string]any{}
	if err := decodeJSON(original, &obj); err != nil {
		return fmt.Errorf("%w: %v", ErrFormat, err)
	}
	removed, err := removeClientHook(obj, entry, command)
	if err != nil {
		return err
	}
	if !removed {
		return nil
	}
	desired, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	p := InstallPlan{Spec: ConfigSpec{Adapter: entry.Adapter, Path: clean, Format: FormatJSON}, Path: clean, Original: original, Desired: desired, Exists: true, Mode: info.Mode(), Changed: true, resolvedDir: resolvedInstallDir(filepath.Dir(clean))}
	return RemoveWithProvenance(p)
}

func addClientHook(obj map[string]any, entry ClientInstallation, command string) error {
	hooks, err := hookObject(obj)
	if err != nil {
		return err
	}
	if entry.ConfigShape == "flat-hooks" {
		if raw, ok := hooks[entry.HookEvent]; ok {
			if _, ok := raw.([]any); !ok {
				return fmt.Errorf("%w: %s must be an array", ErrFormat, entry.HookEvent)
			}
		}
		items, _ := hooks[entry.HookEvent].([]any)
		owned, foreign := findFlatOwnedHook(items, entry.Adapter, command)
		if foreign {
			return ErrOwnership
		}
		if !owned {
			items = append(items, map[string]any{"type": "command", "command": command})
		}
		hooks[entry.HookEvent] = items
		if raw, exists := obj["version"]; exists {
			if version, ok := raw.(float64); !ok || version != 1 {
				return fmt.Errorf("%w: Cursor hooks version must be 1", ErrFormat)
			}
		} else {
			obj["version"] = 1
		}
		obj["hooks"] = hooks
		return nil
	}
	groups, err := clientHookGroups(hooks, entry.HookEvent)
	if err != nil {
		return err
	}
	owned, foreign := findOwnedHook(groups, entry.Adapter, command)
	if foreign {
		return ErrOwnership
	}
	if !owned {
		groups = append(groups, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "name": "autogit"}}})
	}
	hooks[entry.HookEvent] = groups
	obj["hooks"] = hooks
	return nil
}

func removeClientHook(obj map[string]any, entry ClientInstallation, command string) (bool, error) {
	hooks, err := hookObject(obj)
	if err != nil {
		return false, err
	}
	if entry.ConfigShape == "flat-hooks" {
		raw, present := hooks[entry.HookEvent]
		items, exists := raw.([]any)
		if present && !exists {
			return false, fmt.Errorf("%w: %s must be an array", ErrFormat, entry.HookEvent)
		} else if !present {
			items = nil
		}
		kept := make([]any, 0, len(items))
		removed := false
		for _, item := range items {
			hm, ok := item.(map[string]any)
			if ok && hm["command"] == command {
				removed = true
				continue
			}
			kept = append(kept, item)
		}
		if !removed {
			return false, nil
		}
		if len(kept) == 0 {
			delete(hooks, entry.HookEvent)
		} else {
			hooks[entry.HookEvent] = kept
		}
		obj["hooks"] = hooks
		return true, nil
	}
	groups, err := clientHookGroups(hooks, entry.HookEvent)
	if err != nil {
		return false, err
	}
	kept := make([]any, 0, len(groups))
	removed := false
	for _, group := range groups {
		gm, ok := group.(map[string]any)
		if !ok {
			kept = append(kept, group)
			continue
		}
		inner, _ := gm["hooks"].([]any)
		newInner := make([]any, 0, len(inner))
		for _, raw := range inner {
			hm, ok := raw.(map[string]any)
			if ok && hm["name"] == "autogit" && hm["command"] == command {
				removed = true
				continue
			}
			newInner = append(newInner, raw)
		}
		if len(newInner) != 0 {
			gm["hooks"] = newInner
			kept = append(kept, gm)
		} else if len(inner) == 0 || !removed {
			kept = append(kept, gm)
		}
	}
	if !removed {
		return false, nil
	}
	if len(kept) == 0 {
		delete(hooks, entry.HookEvent)
	} else {
		hooks[entry.HookEvent] = kept
	}
	obj["hooks"] = hooks
	return true, nil
}

func clientHookGroups(hooks map[string]any, event string) ([]any, error) {
	raw, exists := hooks[event]
	if !exists {
		return nil, nil
	}
	groups, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an array", ErrFormat, event)
	}
	for _, rawGroup := range groups {
		group, ok := rawGroup.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: %s entries must be objects", ErrFormat, event)
		}
		inner, exists := group["hooks"]
		if !exists {
			return nil, fmt.Errorf("%w: %s entries must contain hooks", ErrFormat, event)
		}
		if _, ok := inner.([]any); !ok {
			return nil, fmt.Errorf("%w: %s entry hooks must be an array", ErrFormat, event)
		}
	}
	return groups, nil
}

func hookObject(obj map[string]any) (map[string]any, error) {
	if raw, ok := obj["hooks"]; ok {
		hooks, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: hooks must be an object", ErrFormat)
		}
		return hooks, nil
	}
	hooks := map[string]any{}
	obj["hooks"] = hooks
	return hooks, nil
}

func findOwnedHook(raw any, adapter, command string) (owned, foreign bool) {
	groups, _ := raw.([]any)
	for _, group := range groups {
		gm, ok := group.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := gm["hooks"].([]any)
		for _, item := range inner {
			hm, ok := item.(map[string]any)
			if !ok || hm["name"] != "autogit" {
				continue
			}
			if hm["command"] == command {
				owned = true
			} else {
				foreign = true
			}
		}
	}
	return
}

func findFlatOwnedHook(items []any, adapter, command string) (owned, foreign bool) {
	prefix := "autogit hook --adapter " + adapter + " "
	for _, item := range items {
		hm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		value, _ := hm["command"].(string)
		if value == command {
			owned = true
		} else if strings.HasPrefix(value, prefix) {
			foreign = true
		}
	}
	return
}
