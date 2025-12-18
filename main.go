package main

import (
	"errors"
	"os"

	"xmlui-mcp/pkg/xmluimcp"
	"xmlui/commands/newcmd"
	"xmlui/commands/runcmd"
	"xmlui/utils"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "xmlui",
	Short: "An all-in-one tool for working with XMLUI.",
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

func init() {
	cobra.EnableCommandSorting = false
	setupMcpCmd()
	setupListCmd()
	setupRunCmd()
	setupNewCmd()
	rootCmd.CompletionOptions.DisableDefaultCmd = true
}

var (
	mcpExampleDirs  []string
	mcpPort         string
	mcpHTTPMode     bool
	mcpXMLUIVersion string

	runPort string

	newOutput string
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
