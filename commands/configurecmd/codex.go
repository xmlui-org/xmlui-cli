package configurecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

var errCodexNotInstalled = errors.New("codex CLI not installed")

// codexEntry mirrors the JSON shape produced by `codex mcp list --json`.
// Only the fields the doctor reports on are pulled out; everything else is
// ignored so codex schema additions don't break parsing.
type codexEntry struct {
	Name           string         `json:"name"`
	Enabled        bool           `json:"enabled"`
	DisabledReason *string        `json:"disabled_reason"`
	Transport      codexTransport `json:"transport"`
}

type codexTransport struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	URL     string   `json:"url"`
}

// parseCodexRegistrations parses the JSON array from `codex mcp list --json`
// and returns the xmlui entries (if any).
func parseCodexRegistrations(data []byte) ([]registration, error) {
	var entries []codexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse codex mcp list output: %w", err)
	}
	var out []registration
	for _, e := range entries {
		if e.Name != serverName {
			continue
		}
		reg := registration{
			source:        "  codex user scope",
			command:       e.Transport.Command,
			args:          e.Transport.Args,
			transportType: e.Transport.Type,
			url:           e.Transport.URL,
			enabled:       e.Enabled,
			isCodex:       true,
		}
		if e.DisabledReason != nil {
			reg.disabledReason = *e.DisabledReason
		}
		out = append(out, reg)
	}
	return out, nil
}

// scanCodex shells out to `codex mcp list --json` and returns xmlui entries.
// Returns errCodexNotInstalled if the codex CLI is not on PATH so the doctor
// can present a clean "skipping" line instead of a noisy error.
func scanCodex() ([]registration, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return nil, errCodexNotInstalled
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "codex", "mcp", "list", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("codex mcp list --json failed: %w", err)
	}
	return parseCodexRegistrations(out)
}
