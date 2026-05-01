package installcmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"xmlui/utils"
)

// Options configure the install command.
type Options struct {
	Prefix    string
	AddToPath bool
}

// HandleInstallCmd copies the running binary to a directory on PATH (or to
// the requested --prefix), strips the macOS quarantine xattr, and prints the
// next step. Idempotent: safe to re-run.
func HandleInstallCmd(opts Options) {
	exe, err := os.Executable()
	if err != nil {
		utils.ConsoleLogger.Fatalf("Error: could not determine running binary: %v\n", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	binaryName := "xmlui"
	if runtime.GOOS == "windows" {
		binaryName = "xmlui.exe"
	}

	installDir := opts.Prefix
	if installDir == "" {
		installDir = pickInstallDir()
	}
	dest := filepath.Join(installDir, binaryName)

	// If we're already running from the destination, nothing to copy.
	if sameFile(exe, dest) {
		utils.ConsoleLogger.Printf("Already installed at %s\n", dest)
	} else {
		if err := os.MkdirAll(installDir, 0o755); err != nil {
			utils.ConsoleLogger.Fatalf("Error creating %s: %v\n", installDir, err)
		}
		if err := copyFile(exe, dest); err != nil {
			utils.ConsoleLogger.Fatalf("Error copying binary to %s: %v\n", dest, err)
		}
		if err := os.Chmod(dest, 0o755); err != nil {
			utils.ConsoleLogger.Fatalf("Error chmod %s: %v\n", dest, err)
		}
		utils.ConsoleLogger.Printf("Installed: %s\n", dest)
	}

	if runtime.GOOS == "darwin" {
		if err := exec.Command("xattr", "-d", "com.apple.quarantine", dest).Run(); err == nil {
			utils.ConsoleLogger.Println("Removed macOS quarantine attribute.")
		}
	}

	onPath := isOnPath(installDir)
	if !onPath {
		if runtime.GOOS == "windows" {
			if opts.AddToPath {
				if err := appendWindowsUserPath(installDir); err == nil {
					utils.ConsoleLogger.Printf("Ensured %s is on your user PATH.\n", installDir)
					utils.ConsoleLogger.Println("Open a new PowerShell or Command Prompt window for the change to take effect.")
				} else {
					utils.ConsoleLogger.Printf("Could not update your user PATH automatically: %v\n", err)
					utils.ConsoleLogger.Printf("Add %s to your user PATH manually.\n", installDir)
				}
			} else {
				utils.ConsoleLogger.Printf("\n%s is not on your PATH.\n", installDir)
				utils.ConsoleLogger.Printf("Add %s to your user PATH manually,\n", installDir)
				utils.ConsoleLogger.Println("or re-run with --add-to-path to do it automatically.")
			}
		} else {
			shellRC, line := pathExportLine(installDir)
			if opts.AddToPath && shellRC != "" {
				if appendIfMissing(shellRC, line) {
					utils.ConsoleLogger.Printf("Added %s to PATH via %s.\n", installDir, shellRC)
					utils.ConsoleLogger.Println("Open a new terminal tab for the change to take effect.")
				}
			} else {
				utils.ConsoleLogger.Printf("\n%s is not on your PATH.\n", installDir)
				if shellRC != "" {
					utils.ConsoleLogger.Printf("Add this line to %s and start a new shell:\n", shellRC)
					utils.ConsoleLogger.Printf("  %s\n", line)
					utils.ConsoleLogger.Println("\nOr re-run with --add-to-path to do it automatically.")
				} else {
					utils.ConsoleLogger.Printf("Add %s to your PATH manually.\n", installDir)
				}
			}
		}
	}

	utils.ConsoleLogger.Println("\nNext, if you are using Claude Code or Codex, register the MCP server:")
	utils.ConsoleLogger.Println("  claude mcp add --scope user xmlui xmlui mcp")
	utils.ConsoleLogger.Println("  codex mcp add xmlui -- xmlui mcp")
}

// pickInstallDir prefers /usr/local/bin if it's writable (or doesn't exist
// yet but the parent is). Falls back to ~/.local/bin (or ~/bin on Windows).
func pickInstallDir() string {
	if runtime.GOOS == "windows" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "bin")
	}
	if isWritable("/usr/local/bin") {
		return "/usr/local/bin"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
}

func isWritable(dir string) bool {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return false
		}
		// Try a probe write.
		f, err := os.CreateTemp(dir, ".xmlui-probe-*")
		if err != nil {
			return false
		}
		name := f.Name()
		f.Close()
		os.Remove(name)
		return true
	}
	// Doesn't exist; check parent.
	parent := filepath.Dir(dir)
	if info, err := os.Stat(parent); err == nil && info.IsDir() {
		return isWritable(parent)
	}
	return false
}

func isOnPath(dir string) bool {
	target, err := filepath.EvalSymlinks(dir)
	if err != nil {
		target = dir
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			resolved = p
		}
		if resolved == target {
			return true
		}
	}
	return false
}

// pathExportLine returns the shell rc file to append to and the line that
// would put dir on PATH. Returns ("", "") on Windows.
func pathExportLine(dir string) (rcFile, line string) {
	if runtime.GOOS == "windows" {
		return "", ""
	}
	home, _ := os.UserHomeDir()
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "zsh":
		rcFile = filepath.Join(home, ".zshrc")
	case "bash":
		// Mac uses ~/.bash_profile by convention; Linux uses ~/.bashrc.
		if runtime.GOOS == "darwin" {
			rcFile = filepath.Join(home, ".bash_profile")
		} else {
			rcFile = filepath.Join(home, ".bashrc")
		}
	default:
		rcFile = filepath.Join(home, ".profile")
	}
	line = fmt.Sprintf(`export PATH="%s:$PATH"`, dir)
	return rcFile, line
}

func appendIfMissing(path, line string) bool {
	existing, err := os.ReadFile(path)
	if err == nil && strings.Contains(string(existing), line) {
		return false
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	defer f.Close()
	fmt.Fprintf(f, "\n# Added by xmlui install\n%s\n", line)
	return true
}

func appendWindowsUserPath(dir string) error {
	script := fmt.Sprintf(
		`$dir = %q; `+
			`$current = [Environment]::GetEnvironmentVariable('Path', 'User'); `+
			`$parts = @(); `+
			`if ($current) { $parts = $current -split ';' | Where-Object { $_ } } `+
			`if ($parts -notcontains $dir) { `+
			`$newValue = if ($current -and $current.TrimEnd(';')) { $current.TrimEnd(';') + ';' + $dir } else { $dir }; `+
			`[Environment]::SetEnvironmentVariable('Path', $newValue, 'User') }`,
		dir,
	)
	return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".xmlui-install-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, dst)
}

func sameFile(a, b string) bool {
	infoA, err := os.Stat(a)
	if err != nil {
		return false
	}
	infoB, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}
