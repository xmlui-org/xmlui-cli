package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"xmlui-mcp/pkg/xmluimcp"
	"xmlui/commands/newcmd"
	"xmlui/commands/runcmd"

	"github.com/spf13/cobra"
)

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

		runcmd.HandleRunCmd(runcmd.Options{ClientDir: clientDir, ServerPort: runPort})
	},
}

var newCmd = &cobra.Command{
	Use:   "new [template]",
	Short: "Creates a new project based on a template",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		templateOrSubcmdName := args[0]

		subcmds := cmd.Commands()
		for _, subcmd := range subcmds {
			if subcmd.Name() == templateOrSubcmdName {
				subcmd.Execute()
				return
			}
		}

		newcmd.HandleNewCmd(newcmd.Options{
			TemplateName: templateOrSubcmdName,
			OutputDir:    newOutput,
		})
	},
}

var newListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists the available templates",
	Run: func(cmd *cobra.Command, args []string) {
		newcmd.HandleNewListCmd()
	},
}

func init() {
	setupMcpCmd()
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

func setupNewCmd() {
	newCmd.Flags().StringVarP(&newOutput, "output", "o", "", "Output directory")
	newCmd.AddCommand(newListCmd)
	rootCmd.AddCommand(newCmd)
}

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
