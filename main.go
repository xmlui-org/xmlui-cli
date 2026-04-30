package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime"

	"xmlui-mcp/pkg/xmluimcp"
	"xmlui/commands/configurecmd"
	"xmlui/commands/distillcmd"
	"xmlui/commands/installcmd"
	"xmlui/commands/newcmd"
	"xmlui/commands/runcmd"
	"xmlui/utils"

	"github.com/spf13/cobra"
)

// version is set via -ldflags at build time.
var version = "dev"

func main() {
	// On Windows, if run without arguments (e.g., double-clicked), show help and wait
	shouldPause := runtime.GOOS == "windows" && len(os.Args) == 1

	if err := rootCmd.Execute(); err != nil {
		if shouldPause {
			pauseBeforeExit()
		}
		os.Exit(1)
	}

	if shouldPause {
		pauseBeforeExit()
	}
}

func pauseBeforeExit() {
	fmt.Println("\nPress Enter to exit...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

var rootCmd = &cobra.Command{
	Use:     "xmlui",
	Short:   "An all-in-one tool for working with XMLUI.",
	Version: version,
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
			CLIVersion:   version,
		}

		utils.ConsoleLogger.Printf("Initializing MCP server...\n")
		server, err := xmluimcp.NewServer(config)
		if err != nil {
			if errors.Is(err, xmluimcp.ErrVersionNotFound) && mcpXMLUIVersion != "" {
				utils.ConsoleLogger.Printf("\nError: The specified XMLUI version '%s' was not found.\nPlease check if it is a valid version.\n", mcpXMLUIVersion)
				os.Exit(1)
			}
			utils.ConsoleLogger.Fatalf("Failed to create XMLUI MCP server: %v", err)
		}
		utils.ConsoleLogger.Printf("Initialization Done!\n")
		if mcpHTTPMode {
			if err := server.ServeHTTP(); err != nil {
				utils.ConsoleLogger.Fatalf("Server error: %v", err)
			}
		} else {
			if err := server.ServeStdio(); err != nil {
				utils.ConsoleLogger.Fatalf("Stdio server error: %v", err)
			}
		}
	},
}

var runCmd = &cobra.Command{
	Use:   "run [dir|zipfile|url]",
	Short: "Runs an XMLUI app",
	Example: `# Acquire and run from an URL
$ xmlui run https://github.com/xmlui-org/xmlui-hello-world/releases/latest/download/xmlui-hello-world.zip

# Run the app in the current directory
$ cd ~/xmlui-weather
$ xmlui run

# Unzip an existing file and run it
$ xmlui run xmlui-hello-world.zip`,
	Long: `Runs the XMLUI app in the current working directory or specified directory.
If a local or remote zip file is specified, it will be extracted and run.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runTarget := ""
		if len(args) > 0 {
			runTarget = args[0]
		}

		runcmd.HandleRunCmd(runcmd.Options{RunTarget: runTarget, ServerPort: runPort})
	},
}

var newCmd = &cobra.Command{
	Use:     "new [app]",
	Short:   "Creates a new project based on an existing XMLUI app",
	Example: `$ xmlui new xmlui-weather`,
	Long:    `Creates a new project based on an existing XMLUI app found via "xmlui list-demos"`,
	Args:    cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		newcmd.HandleNewCmd(newcmd.Options{
			TemplateName: args[0],
			OutputDir:    newOutput,
		})
	},
}

var listDemosCmd = &cobra.Command{
	Use:   "list-demos",
	Short: "Lists the available demo apps",
	Long: `Lists the available demo apps.
Use one of the returned apps as the argument for the "xmlui new" command.`,
	Run: func(cmd *cobra.Command, args []string) {
		newcmd.HandleListCmd()
	},
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Copies the xmlui binary into a directory on PATH",
	Long: `Copies the running xmlui binary into a directory on PATH so 'xmlui'
becomes available in any shell. Picks /usr/local/bin if writable, else
~/.local/bin (or ~/bin on Windows). Use --prefix to override.

If the install directory is not on PATH, prints the line to add to your
shell rc; pass --add-to-path to append it automatically. On Windows,
--add-to-path updates the user PATH.`,
	Example: `# Default install
$ xmlui install

# Install to a specific directory
$ xmlui install --prefix ~/.local/bin

# Auto-add the install dir to your shell rc
$ xmlui install --add-to-path`,
	Run: func(cmd *cobra.Command, args []string) {
		installcmd.HandleInstallCmd(installcmd.Options{
			Prefix:    installPrefix,
			AddToPath: installAddToPath,
		})
	},
}

var distillTraceCmd = &cobra.Command{
	Use:   "distill-trace [path]",
	Short: "Distills an XMLUI Inspector trace into per-step user-journey JSON",
	Long: `Reads an exported XMLUI Inspector trace (JSON array of log events) and
writes a per-step distillation to stdout. By default it emits detailed JSON;
pass --summary for a more compact analysis-oriented form. If no path is given,
uses the most recent xs-trace-*.json found in ~/Downloads.`,
	Example: `# Distill the most recent trace
$ xmlui distill-trace

# Distill a compact summary
$ xmlui distill-trace --summary

# Distill a specific trace
$ xmlui distill-trace ~/Downloads/xs-trace-20260428T233834.json`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := ""
		if len(args) > 0 {
			path = args[0]
		}
		distillcmd.HandleDistillCmd(distillcmd.Options{
			Path:    path,
			Summary: distillTraceSummary,
		})
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnoses xmlui MCP server registrations across known config locations",
	Long: `Scans the user-scope and project-scope locations where an xmlui MCP server
entry could be registered, validates each binary path, and runs --version
against each binary. Use after 'claude mcp add --scope user xmlui xmlui mcp'
to confirm the registration took, or when Claude doesn't see the xmlui tools.`,
	Run: func(cmd *cobra.Command, args []string) {
		configurecmd.HandleDoctorCmd(configurecmd.DoctorOptions{})
	},
}

func init() {
	cobra.EnableCommandSorting = false
	setupMcpCmd()
	setupListCmd()
	setupRunCmd()
	setupNewCmd()
	setupDoctorCmd()
	setupDistillTraceCmd()
	setupInstallCmd()
	rootCmd.CompletionOptions.DisableDefaultCmd = true
}

var (
	mcpExampleDirs  []string
	mcpPort         string
	mcpHTTPMode     bool
	mcpXMLUIVersion string

	runPort string

	newOutput string

	installPrefix    string
	installAddToPath bool

	distillTraceSummary bool
)

func setupListCmd() {
	rootCmd.AddCommand(listDemosCmd)
}

func setupNewCmd() {
	newCmd.Flags().StringVarP(&newOutput, "output", "o", "", "`<path>` to output directory")
	rootCmd.AddCommand(newCmd)
}

func setupMcpCmd() {
	mcpCmd.Flags().StringSliceVarP(&mcpExampleDirs, "example", "e", []string{}, "`<path>` to example directory (option can be repeated)")
	mcpCmd.Flags().StringVarP(&mcpPort, "port", "p", "9090", "`<port>` to run the HTTP server on (used only with http mode)")
	mcpCmd.Flags().BoolVar(&mcpHTTPMode, "http", false, "Run as HTTP server")
	mcpCmd.Flags().StringVar(&mcpXMLUIVersion, "xmlui-version", "", "Specific XMLUI `<version>` to use for documentation (e.g. 0.11.4)")
	rootCmd.AddCommand(mcpCmd)
}

func setupRunCmd() {
	runCmd.Flags().StringVarP(&runPort, "port", "p", "", "`<port>` to run the HTTP server on.\nDefaults to 8080 or to a random port when 8080 is taken. ")
	rootCmd.AddCommand(runCmd)
}

func setupDistillTraceCmd() {
	distillTraceCmd.Flags().BoolVar(&distillTraceSummary, "summary", false, "Emit a compact summary JSON instead of the full detailed distillation")
	rootCmd.AddCommand(distillTraceCmd)
}

func setupDoctorCmd() {
	rootCmd.AddCommand(doctorCmd)
}

func setupInstallCmd() {
	installCmd.Flags().StringVar(&installPrefix, "prefix", "", "`<dir>` to install into (default: /usr/local/bin if writable, else ~/.local/bin)")
	installCmd.Flags().BoolVar(&installAddToPath, "add-to-path", false, "Update PATH automatically if needed (shell rc on Unix, user PATH on Windows)")
	rootCmd.AddCommand(installCmd)
}
