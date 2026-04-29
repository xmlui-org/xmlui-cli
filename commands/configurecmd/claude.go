package configurecmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"xmlui/utils"
)

const serverName = "xmlui"

// Scope mirrors `claude mcp add --scope` semantics.
type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeLocal   Scope = "local"
	ScopeProject Scope = "project"
)

type ClaudeOptions struct {
	Remove bool
	Scope  Scope
}

// mcpEntry matches the JSON shape `claude mcp add` writes.
type mcpEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

func newEntry(binary string) *mcpEntry {
	return &mcpEntry{
		Type:    "stdio",
		Command: binary,
		Args:    []string{"mcp"},
		Env:     map[string]string{},
	}
}

// HandleConfigureClaudeCmd registers (or unregisters) the xmlui MCP server in
// ~/.claude.json — the canonical Claude Code config file — at the requested
// scope. Default scope is "user" (top-level mcpServers) which matches
// `claude mcp add --scope user` and makes the server available across all
// projects.
func HandleConfigureClaudeCmd(opts ClaudeOptions) {
	scope := opts.Scope
	if scope == "" {
		scope = ScopeUser
	}

	binary, err := resolveBinary()
	if err != nil {
		utils.ConsoleLogger.Fatalf("Error: could not determine xmlui binary path: %v\n", err)
	}

	switch scope {
	case ScopeUser:
		handleClaudeJSONScope(opts, binary, scope)
	case ScopeLocal:
		handleClaudeJSONScope(opts, binary, scope)
	case ScopeProject:
		handleProjectScope(opts, binary)
	default:
		utils.ConsoleLogger.Fatalf("Error: unknown scope %q (use user, local, or project)\n", string(scope))
	}
}

func handleClaudeJSONScope(opts ClaudeOptions, binary string, scope Scope) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		utils.ConsoleLogger.Fatalf("Error: could not determine home directory: %v\n", err)
	}
	path := filepath.Join(homeDir, ".claude.json")

	root, err := readJSON(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			utils.ConsoleLogger.Fatalf("Error reading %s: %v\n", path, err)
		}
		root = map[string]interface{}{}
	}

	servers, locationLabel := getOrCreateClaudeJSONServers(root, scope)
	_, exists := servers[serverName]

	if opts.Remove {
		if !exists {
			utils.ConsoleLogger.Println("No xmlui MCP server registered. Nothing to remove.")
			return
		}
		delete(servers, serverName)
		setClaudeJSONServers(root, scope, servers)
		if err := writeJSONAtomic(path, root); err != nil {
			utils.ConsoleLogger.Fatalf("Error updating %s: %v\n", path, err)
		}
		utils.ConsoleLogger.Printf("Removed xmlui MCP server from %s (%s)\n", path, locationLabel)
		utils.ConsoleLogger.Println("Restart Claude Code to apply.")
		return
	}

	servers[serverName] = newEntry(binary)
	setClaudeJSONServers(root, scope, servers)
	if err := writeJSONAtomic(path, root); err != nil {
		utils.ConsoleLogger.Fatalf("Error updating %s: %v\n", path, err)
	}
	if exists {
		utils.ConsoleLogger.Printf("Updated xmlui MCP server in %s (%s)\n", path, locationLabel)
	} else {
		utils.ConsoleLogger.Printf("Registered xmlui MCP server in %s (%s)\n", path, locationLabel)
	}
	utils.ConsoleLogger.Println("Restart Claude Code to apply.")
}

func handleProjectScope(opts ClaudeOptions, binary string) {
	cwd, err := os.Getwd()
	if err != nil {
		utils.ConsoleLogger.Fatalf("Error: could not determine working directory: %v\n", err)
	}
	path := filepath.Join(cwd, ".mcp.json")

	root, err := readJSON(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			utils.ConsoleLogger.Fatalf("Error reading %s: %v\n", path, err)
		}
		root = map[string]interface{}{}
	}

	servers := getProjectMCPServers(root)
	_, exists := servers[serverName]

	if opts.Remove {
		if !exists {
			utils.ConsoleLogger.Println("No xmlui MCP server registered. Nothing to remove.")
			return
		}
		delete(servers, serverName)
		root["mcpServers"] = servers
		if err := writeJSONAtomic(path, root); err != nil {
			utils.ConsoleLogger.Fatalf("Error updating %s: %v\n", path, err)
		}
		utils.ConsoleLogger.Printf("Removed xmlui MCP server from %s\n", path)
		utils.ConsoleLogger.Println("Restart Claude Code to apply.")
		return
	}

	servers[serverName] = newEntry(binary)
	root["mcpServers"] = servers
	if err := writeJSONAtomic(path, root); err != nil {
		utils.ConsoleLogger.Fatalf("Error updating %s: %v\n", path, err)
	}
	if exists {
		utils.ConsoleLogger.Printf("Updated xmlui MCP server in %s\n", path)
	} else {
		utils.ConsoleLogger.Printf("Registered xmlui MCP server in %s\n", path)
	}
	utils.ConsoleLogger.Println("Restart Claude Code to apply.")
}

// getOrCreateClaudeJSONServers returns the mcpServers map for the given scope
// in ~/.claude.json, creating intermediate keys if needed. The second return
// is a human-readable label for messages.
func getOrCreateClaudeJSONServers(root map[string]interface{}, scope Scope) (map[string]interface{}, string) {
	switch scope {
	case ScopeUser:
		servers, _ := root["mcpServers"].(map[string]interface{})
		if servers == nil {
			servers = map[string]interface{}{}
		}
		return servers, "user scope"
	case ScopeLocal:
		cwd, _ := os.Getwd()
		projects, _ := root["projects"].(map[string]interface{})
		if projects == nil {
			projects = map[string]interface{}{}
			root["projects"] = projects
		}
		entry, _ := projects[cwd].(map[string]interface{})
		if entry == nil {
			entry = map[string]interface{}{}
			projects[cwd] = entry
		}
		servers, _ := entry["mcpServers"].(map[string]interface{})
		if servers == nil {
			servers = map[string]interface{}{}
		}
		return servers, "local scope: " + cwd
	}
	return nil, ""
}

func setClaudeJSONServers(root map[string]interface{}, scope Scope, servers map[string]interface{}) {
	switch scope {
	case ScopeUser:
		root["mcpServers"] = servers
	case ScopeLocal:
		cwd, _ := os.Getwd()
		projects, _ := root["projects"].(map[string]interface{})
		if projects == nil {
			projects = map[string]interface{}{}
			root["projects"] = projects
		}
		entry, _ := projects[cwd].(map[string]interface{})
		if entry == nil {
			entry = map[string]interface{}{}
			projects[cwd] = entry
		}
		entry["mcpServers"] = servers
	}
}

func getProjectMCPServers(root map[string]interface{}) map[string]interface{} {
	servers, _ := root["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	return servers
}

func resolveBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil
	}
	return resolved, nil
}

func readJSON(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return map[string]interface{}{}, nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if root == nil {
		root = map[string]interface{}{}
	}
	return root, nil
}

func writeJSONAtomic(path string, root map[string]interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	tmp, err := os.CreateTemp(dir, ".xmlui-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
