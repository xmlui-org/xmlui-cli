package configurecmd

import "testing"

func TestParseCodexRegistrationsFindsXMLUI(t *testing.T) {
	data := []byte(`[
  {
    "name": "filesystem",
    "enabled": true,
    "disabled_reason": null,
    "transport": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem"]
    }
  },
  {
    "name": "xmlui",
    "enabled": true,
    "disabled_reason": null,
    "transport": {
      "type": "stdio",
      "command": "xmlui",
      "args": ["mcp"]
    }
  }
]`)

	regs, err := parseCodexRegistrations(data)
	if err != nil {
		t.Fatalf("parseCodexRegistrations returned error: %v", err)
	}
	if len(regs) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(regs))
	}

	reg := regs[0]
	if reg.source != "  codex user scope" {
		t.Fatalf("unexpected source: %q", reg.source)
	}
	if reg.command != "xmlui" {
		t.Fatalf("unexpected command: %q", reg.command)
	}
	if len(reg.args) != 1 || reg.args[0] != "mcp" {
		t.Fatalf("unexpected args: %v", reg.args)
	}
	if reg.transportType != "stdio" {
		t.Fatalf("unexpected transport: %q", reg.transportType)
	}
	if !reg.enabled {
		t.Fatal("expected registration to be enabled")
	}
}

func TestParseCodexRegistrationsPreservesDisabledState(t *testing.T) {
	data := []byte(`[
  {
    "name": "xmlui",
    "enabled": false,
    "disabled_reason": "startup timeout",
    "transport": {
      "type": "stdio",
      "command": "xmlui",
      "args": ["mcp"]
    }
  }
]`)

	regs, err := parseCodexRegistrations(data)
	if err != nil {
		t.Fatalf("parseCodexRegistrations returned error: %v", err)
	}
	if len(regs) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(regs))
	}
	if regs[0].enabled {
		t.Fatal("expected registration to be disabled")
	}
	if regs[0].disabledReason != "startup timeout" {
		t.Fatalf("unexpected disabled reason: %q", regs[0].disabledReason)
	}
}
