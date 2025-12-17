package runcmd

import (
	"archive/zip"
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

	clientDir := opts.ClientDir
	if strings.HasSuffix(strings.ToLower(opts.ClientDir), ".zip") {

		destDir, err := os.Getwd()
		if err != nil {
			log.Fatalf("Error while getting the current working directory needed to determine where to extract file: %s", err.Error())
		}

		extractedDir, err := handleZipArg(opts.ClientDir, destDir)
		if err != nil {
			log.Fatal(err.Error())
		} else {
			clientDir = extractedDir
		}
	}

	if startScriptPath, err := getStartScript(clientDir); err == nil {
		fmt.Printf("Executing found start script at: %s\n", startScriptPath)
		err := execPassOwnership(startScriptPath)
		if err != nil {
			log.Fatalf("Error running start script: %s", err.Error())
		}
		return
	}

	config := ServerConfig{
		Dir:  clientDir,
		Port: opts.ServerPort,
	}

	if err := Start(config); err != nil {
		log.Fatal(err)
	}
}

func handleZipArg(zipfile string, destDir string) (extractedDir string, err error) {
	baseName := filepath.Base(zipfile)
	ext := filepath.Ext(baseName)
	dirName := baseName[:len(baseName)-len(ext)]
	targetDir := filepath.Join(destDir, dirName)

	if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
		entries, err := os.ReadDir(destDir)
		if err != nil {
			return "", fmt.Errorf("Failed to read directory %s: %v", destDir, err)
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
		targetDir = filepath.Join(destDir, fmt.Sprintf("%s-%d", dirName, maxNum+1))
	}

	fmt.Printf("Extracting %s to %s...\n", zipfile, targetDir)
	r, err := zip.OpenReader(zipfile)
	if err != nil {
		return "", fmt.Errorf("Failed to open zip file: %v", err)
	}
	err = utils.Unzip(&r.Reader, targetDir)
	r.Close()
	if err != nil {
		return "", fmt.Errorf("Failed to extract zip file: %v", err)
	}
	return targetDir, nil
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

// Runs a file which takes ownership of std-in,out,err.
// Caller exits with the same exit code.
//
// Only returns errors when spawning the process fails.
func execPassOwnership(path string) error {
	cmd := exec.Command(path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		} else {
			return err
		}
	}
	os.Exit(0)
	return nil
}
