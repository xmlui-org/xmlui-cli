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

var distillTraceCmd = &cobra.Command{
	Use:   "distill-trace [path]",
	Short: "Distills an XMLUI Inspector trace into per-step user-journey JSON",
	Long: `Reads an exported XMLUI Inspector trace (JSON array of log events) and
writes a per-step distillation to stdout. If no path is given, uses the most
recent xs-trace-*.json found in ~/Downloads.`,
	Example: `# Distill the most recent trace
$ xmlui distill-trace

# Distill a specific trace
$ xmlui distill-trace ~/Downloads/xs-trace-20260428T233834.json`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := ""
		if len(args) > 0 {
			path = args[0]
		}
		distillcmd.HandleDistillCmd(distillcmd.Options{Path: path})
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnoses xmlui MCP server registrations across known config locations",
	Long: `Scans the user-scope and project-scope locations where an xmlui MCP server
entry could be registered, validates each binary path, and runs --version
against each binary. Use after 'configure-claude' to confirm the registration
took, or when the agent doesn't see the xmlui tools.`,
	Run: func(cmd *cobra.Command, args []string) {
		configurecmd.HandleDoctorCmd(configurecmd.DoctorOptions{})
	},
}

var configureClaudeCmd = &cobra.Command{
	Use:   "configure-claude",
	Short: "Registers xmlui as an MCP server with Claude Code",
	Long: `Registers (or updates) the xmlui MCP server in Claude Code's
configuration so the agent can use the xmlui MCP tools.

Scopes mirror 'claude mcp add --scope':
  user    (default) — ~/.claude.json#mcpServers, available in every project
  local             — ~/.claude.json#projects.<cwd>.mcpServers, this project only
  project           — ./.mcp.json#mcpServers, committed in-repo`,
	Example: `# Register at user scope (default)
$ xmlui configure-claude

# Register only for the current project
$ xmlui configure-claude --scope local

# Write to ./.mcp.json (committable)
$ xmlui configure-claude --scope project

# Unregister at the chosen scope
$ xmlui configure-claude --remove`,
	Run: func(cmd *cobra.Command, args []string) {
		configurecmd.HandleConfigureClaudeCmd(configurecmd.ClaudeOptions{
			Remove: configureClaudeRemove,
			Scope:  configurecmd.Scope(configureClaudeScope),
		})
	},
}
func init() {
	cobra.EnableCommandSorting = false
	setupMcpCmd()
	setupListCmd()
	setupRunCmd()
	setupNewCmd()
	setupConfigureClaudeCmd()
	setupDoctorCmd()
	setupDistillTraceCmd()
	rootCmd.CompletionOptions.DisableDefaultCmd = true
}

var (
	mcpExampleDirs  []string
	mcpPort         string
	mcpHTTPMode     bool
	mcpXMLUIVersion string

	runPort string

	newOutput string

	configureClaudeRemove bool
	configureClaudeScope  string
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
	rootCmd.AddCommand(distillTraceCmd)
}

func setupDoctorCmd() {
	rootCmd.AddCommand(doctorCmd)
}

func setupConfigureClaudeCmd() {
	configureClaudeCmd.Flags().BoolVar(&configureClaudeRemove, "remove", false, "Unregister the xmlui MCP server")
	configureClaudeCmd.Flags().StringVar(&configureClaudeScope, "scope", "user", "Configuration scope: user, local, or project")
	rootCmd.AddCommand(configureClaudeCmd)
}
