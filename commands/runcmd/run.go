package runcmd

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"xmlui/utils"
)

type Options struct {
	ClientDir  string
	ServerPort string
}

func HandleRunCmd(opts Options) {
	if strings.HasSuffix(strings.ToLower(opts.ClientDir), ".zip") {
		baseName := filepath.Base(opts.ClientDir)
		ext := filepath.Ext(baseName)
		dirName := baseName[:len(baseName)-len(ext)]
		parentDir := filepath.Dir(opts.ClientDir)
		targetDir := filepath.Join(parentDir, dirName)

		if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
			entries, err := os.ReadDir(parentDir)
			if err != nil {
				log.Fatalf("Failed to read directory %s: %v", parentDir, err)
			}

			maxNum := 0
			prefix := dirName + "-"

			for _, entry := range entries {
				name := entry.Name()
				if strings.HasPrefix(name, prefix) {
					if num, err := strconv.Atoi(name[len(prefix):]); err == nil {
						if num > maxNum {
							maxNum = num
						}
					}
				}
			}
			targetDir = filepath.Join(parentDir, fmt.Sprintf("%s-%d", dirName, maxNum+1))
		}

		fmt.Printf("Extracting %s to %s...\n", opts.ClientDir, targetDir)
		if err := utils.Unzip(opts.ClientDir, targetDir); err != nil {
			log.Fatalf("Failed to extract zip file: %v", err)
		}
		opts.ClientDir = targetDir
	}

	// Run a start script instead of the server if the directory has one
	if startScriptPath, err := getStartScript(opts.ClientDir); err == nil {
		fmt.Printf("Executing found start script at: %s\n", startScriptPath)
		cmd := exec.Command(startScriptPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Printf("Failed to execute start script: %v", err)
			os.Exit(1)
		}
		return
	}

	config := ServerConfig{
		Dir:  opts.ClientDir,
		Port: opts.ServerPort,
	}

	if err := Start(config); err != nil {
		log.Fatal(err)
	}
}

func getStartScript(clientDir string) (startScriptPath string, err error) {
	ensureRelative := func(p string) string {
		if !filepath.IsAbs(p) && !strings.HasPrefix(p, "."+string(os.PathSeparator)) && !strings.HasPrefix(p, ".."+string(os.PathSeparator)) {
			return "." + string(os.PathSeparator) + p
		}
		return p
	}

	if runtime.GOOS == "windows" {
		powShellScript := filepath.Join(clientDir, "start.ps1")
		if info, err := os.Stat(powShellScript); err == nil && !info.IsDir() {
			return ensureRelative(powShellScript), nil
		}

		batchScript := filepath.Join(clientDir, "start.bat")
		if info, err := os.Stat(batchScript); err == nil && !info.IsDir() {
			return ensureRelative(batchScript), nil
		}
	} else {
		shScript := filepath.Join(clientDir, "start.sh")
		if info, err := os.Stat(shScript); err == nil && !info.IsDir() {
			return ensureRelative(shScript), nil
		}
	}

	return "", errors.New("no start script found")
}
