package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"xmlui-mcp/pkg/xmluimcp"
	"xmlui/pkg/server"

	"github.com/spf13/cobra"
)

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

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("%s: illegal file path", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "xmlui",
	Short: "An all-in-one tool for working with xmlui.",
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Starts the model context protocol server",
	Run: func(cmd *cobra.Command, args []string) {

		// Configure the XMLUI MCP server
		config := xmluimcp.ServerConfig{
			ExampleDirs:  mcpExampleDirs,
			HTTPMode:     mcpHTTPMode,
			Port:         mcpPort,
			XMLUIVersion: mcpXMLUIVersion,
		}

		fmt.Fprintf(os.Stderr, "Initializing MCP server...\n")
		server, err := xmluimcp.NewServer(config)
		if err != nil {
			if errors.Is(err, xmluimcp.ErrVersionNotFound) && mcpXMLUIVersion != "" {
				fmt.Fprintf(os.Stderr, "\nError: The specified XMLUI version '%s' was not found.\nPlease check if it is a valid version.\n", mcpXMLUIVersion)
				os.Exit(1)
			}
			log.Fatalf("Failed to create XMLUI MCP server: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Inicialization Done!\n")
		if mcpHTTPMode {
			if err := server.ServeHTTP(); err != nil {
				log.Fatalf("Server error: %v", err)
			}
		} else {
			if err := server.ServeStdio(); err != nil {
				log.Fatalf("Stdio server error: %v", err)
			}
		}
	},
}

var runCmd = &cobra.Command{
	Use:   "run [dir]",
	Short: "Runs the XMLUI server",
	Long: `Runs the XMLUI server at the current working directory, or at the directory specified by the first argument.
If the first argument is a zip file, it will extract the contents next to it and run in that directory.
If the directory contains a start.sh, start.ps1 or start.bat file,
it will run that, instead of starting the server`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		clientDir := ""
		if len(args) > 0 {
			clientDir = args[0]
		}

		if strings.HasSuffix(strings.ToLower(clientDir), ".zip") {
			baseName := filepath.Base(clientDir)
			ext := filepath.Ext(baseName)
			dirName := baseName[:len(baseName)-len(ext)]
			parentDir := filepath.Dir(clientDir)
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

			fmt.Printf("Extracting %s to %s...\n", clientDir, targetDir)
			if err := unzip(clientDir, targetDir); err != nil {
				log.Fatalf("Failed to extract zip file: %v", err)
			}
			clientDir = targetDir
		}

		// Run a start script instead of the server if the directory has one
		if startScriptPath, err := getStartScript(clientDir); err == nil {
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

		config := server.Config{
			Dir:  clientDir,
			Port: runPort,
		}

		if err := server.Start(config); err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	setupMcpCmd()
	setupRunCmd()
	rootCmd.CompletionOptions.DisableDefaultCmd = true
}

var (
	mcpExampleDirs  []string
	mcpPort         string
	mcpHTTPMode     bool
	mcpXMLUIVersion string

	runPort string
)

func setupMcpCmd() {
	mcpCmd.Flags().StringSliceVarP(&mcpExampleDirs, "example", "e", []string{}, "`<path>` to example directory (option can be repeated)")
	mcpCmd.Flags().StringVarP(&mcpPort, "port", "p", "9090", "`<port>` to run the HTTP server on")
	mcpCmd.Flags().BoolVar(&mcpHTTPMode, "http", false, "Run as HTTP server")
	mcpCmd.Flags().StringVar(&mcpXMLUIVersion, "xmlui-version", "", "Specific XMLUI `<version>` to use for documentation (e.g. 0.11.4)")
	rootCmd.AddCommand(mcpCmd)
}

func setupRunCmd() {
	runCmd.Flags().StringVarP(&runPort, "port", "p", "", "`<port>` to run the HTTP server on")
	rootCmd.AddCommand(runCmd)
}
