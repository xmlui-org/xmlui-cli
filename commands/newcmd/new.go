package newcmd

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"xmlui/utils"
)

type Options struct {
	TemplateName string
	OutputDir    string
}

func HandleNewCmd(opts Options) {

	if opts.TemplateName != "hello-world" {
		log.Fatalf("Unknown template: %s. Only 'hello-world' is supported.", opts.TemplateName)
	}

	url := "https://github.com/xmlui-org/xmlui-hello-world/releases/download/v1.0.2/xmlui-hello-world.zip"
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = "xmlui-hello-world"
	}

	fmt.Printf("Downloading template %s...\n", opts.TemplateName)
	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Failed to download template: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to download template: %s", resp.Status)
	}

	tmpFile, err := os.CreateTemp("", "xmlui-template-*.zip")
	if err != nil {
		log.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		log.Fatalf("Failed to save template: %v", err)
	}
	tmpFile.Close()

	fmt.Printf("Extracting to %s...\n", outputDir)
	if err := utils.Unzip(tmpFile.Name(), outputDir); err != nil {
		log.Fatalf("Failed to extract template: %v", err)
	}

}
