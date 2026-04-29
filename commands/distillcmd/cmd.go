package distillcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"xmlui/utils"
)

// Options configure the distill-trace command.
type Options struct {
	Path string
}

// HandleDistillCmd reads a trace JSON file and writes the distilled JSON to
// stdout. If Path is empty, falls back to the most recent
// ~/Downloads/xs-trace-*.json.
func HandleDistillCmd(opts Options) {
	path := opts.Path
	if path == "" {
		resolved, err := findLatestTrace()
		if err != nil {
			utils.ConsoleLogger.Fatalf("Error: %v\n", err)
		}
		path = resolved
		utils.ConsoleLogger.Printf("Using most recent trace: %s\n", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		utils.ConsoleLogger.Fatalf("Error reading %s: %v\n", path, err)
	}
	var logs []Event
	if err := json.Unmarshal(data, &logs); err != nil {
		utils.ConsoleLogger.Fatalf("Error parsing %s: %v\n", path, err)
	}

	out := DistillTrace(logs)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		utils.ConsoleLogger.Fatalf("Error writing JSON: %v\n", err)
	}
}

// FindLatestTrace returns the most recent xs-trace-*.json under ~/Downloads.
// Exported for the MCP tool.
func FindLatestTrace() (string, error) {
	return findLatestTrace()
}

func findLatestTrace() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(homeDir, "Downloads")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", dir, err)
	}
	type candidate struct {
		path  string
		mtime int64
	}
	var matches []candidate
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasPrefix(name, "xs-trace-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		matches = append(matches, candidate{
			path:  filepath.Join(dir, name),
			mtime: info.ModTime().Unix(),
		})
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no xs-trace-*.json files found in %s", dir)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].mtime > matches[j].mtime
	})
	return matches[0].path, nil
}

// readJSONFile is shared with the MCP tool path.
func readJSONFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var logs []Event
	if err := json.Unmarshal(data, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// DistillFile reads a trace file and returns the distilled output. Used by the
// MCP tool.
func DistillFile(path string) (DistillOutput, error) {
	logs, err := readJSONFile(path)
	if err != nil {
		return DistillOutput{}, err
	}
	return DistillTrace(logs), nil
}
