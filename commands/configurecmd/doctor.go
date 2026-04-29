package configurecmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"xmlui/utils"
)

type DoctorOptions struct{}

type registration struct {
	source  string // human-readable origin
	command string
	args    []string
}

// HandleDoctorCmd scans every place an xmlui MCP server registration could
// live and reports each one with binary validation and version output.
func HandleDoctorCmd(_ DoctorOptions) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		utils.ConsoleLogger.Fatalf("Error: could not determine home directory: %v\n", err)
	}
	claudeJSON := filepath.Join(homeDir, ".claude.json")

	utils.ConsoleLogger.Printf("~/.claude.json (%s):\n", claudeJSON)
	regs := scanClaudeJSON(claudeJSON)
	if len(regs) == 0 {
		utils.ConsoleLogger.Println("  no xmlui entry at user scope or in any project entry")
	} else {
		for _, r := range regs {
			reportReg(r)
		}
	}
	utils.ConsoleLogger.Println("")

	cwd, err := os.Getwd()
	if err == nil {
		path := filepath.Join(cwd, ".mcp.json")
		utils.ConsoleLogger.Printf("Project scope (%s):\n", path)
		projectRegs := scanProjectMcpJSON(path)
		if len(projectRegs) == 0 {
			utils.ConsoleLogger.Println("  no xmlui entry")
		} else {
			for _, r := range projectRegs {
				reportReg(r)
			}
		}
		utils.ConsoleLogger.Println("")
		regs = append(regs, projectRegs...)
	}

	switch len(regs) {
	case 0:
		utils.ConsoleLogger.Println("Summary: no xmlui MCP server registered. Run 'xmlui configure-claude'.")
	case 1:
		utils.ConsoleLogger.Println("Summary: 1 xmlui MCP server registration found.")
	default:
		utils.ConsoleLogger.Printf("Summary: %d xmlui MCP server registrations found. Multiple registrations may cause duplicate or missing tools — keep one.\n", len(regs))
	}
}

// scanClaudeJSON returns every xmlui server entry found in ~/.claude.json:
// the user-scope entry under top-level mcpServers, and any local-scope entry
// under projects.<path>.mcpServers.
func scanClaudeJSON(path string) []registration {
	root, err := readJSON(path)
	if err != nil {
		return nil
	}

	var out []registration

	if servers, ok := root["mcpServers"].(map[string]interface{}); ok {
		if reg := extractServerEntry(servers, "  user scope"); reg != nil {
			out = append(out, *reg)
		}
	}

	if projects, ok := root["projects"].(map[string]interface{}); ok {
		// Iterate in deterministic order for stable output.
		keys := make([]string, 0, len(projects))
		for k := range projects {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, projectPath := range keys {
			pe, ok := projects[projectPath].(map[string]interface{})
			if !ok {
				continue
			}
			servers, ok := pe["mcpServers"].(map[string]interface{})
			if !ok {
				continue
			}
			label := "  local scope: " + projectPath
			if reg := extractServerEntry(servers, label); reg != nil {
				out = append(out, *reg)
			}
		}
	}

	return out
}

func scanProjectMcpJSON(path string) []registration {
	root, err := readJSON(path)
	if err != nil {
		return nil
	}
	servers, ok := root["mcpServers"].(map[string]interface{})
	if !ok {
		return nil
	}
	if reg := extractServerEntry(servers, "  "+path); reg != nil {
		return []registration{*reg}
	}
	return nil
}

func extractServerEntry(servers map[string]interface{}, source string) *registration {
	v, ok := servers[serverName]
	if !ok {
		return nil
	}
	entry, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	cmd, _ := entry["command"].(string)
	var args []string
	if a, ok := entry["args"].([]interface{}); ok {
		for _, x := range a {
			if s, ok := x.(string); ok {
				args = append(args, s)
			}
		}
	}
	return &registration{source: source, command: cmd, args: args}
}

func reportReg(r registration) {
	utils.ConsoleLogger.Printf("%s\n", r.source)
	utils.ConsoleLogger.Printf("    command: %s\n", r.command)
	utils.ConsoleLogger.Printf("    args:    %v\n", r.args)

	binStatus, version := inspectBinary(r.command)
	utils.ConsoleLogger.Printf("    binary:  %s\n", binStatus)
	if version != "" {
		utils.ConsoleLogger.Printf("    version: %s\n", version)
	}
}

func inspectBinary(command string) (status, version string) {
	if command == "" {
		return "no command set", ""
	}

	info, err := os.Stat(command)
	if err != nil {
		if os.IsNotExist(err) {
			return "MISSING (no such file)", ""
		}
		return fmt.Sprintf("stat error: %v", err), ""
	}
	if info.IsDir() {
		return "MISSING (path is a directory)", ""
	}
	if info.Mode()&0o111 == 0 {
		return "not executable", ""
	}

	base := filepath.Base(command)
	if base == "bash" || base == "sh" || base == "zsh" {
		return "ok (shell wrapper — version not checked)", ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, command, "--version").CombinedOutput()
	if err != nil {
		return "ok (--version failed: " + strings.TrimSpace(err.Error()) + ")", ""
	}
	v := strings.TrimSpace(string(out))
	if i := strings.IndexByte(v, '\n'); i >= 0 {
		v = v[:i]
	}
	return "ok", v
}
