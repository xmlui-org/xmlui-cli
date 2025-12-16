package newcmd

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"

	"xmlui/utils"
)

type Options struct {
	TemplateName string
	OutputDir    string
}

type Template struct {
	UID         string `json:"uid"`
	DisplayName string `json:"displayName"`
	Author      string `json:"author"`
	Description string `json:"description"`
	ZipArchive  string `json:"zipArchive"`
}

type TemplateRegistry struct {
	Templates []Template `json:"templates"`
}

const templatesJson = `{
	"templates": [
		{
			"uid": "hello-world",
			"displayName": "Hello World",
			"author": "xmlui.org",
			"description": "The simplest xmlui app to get you started.",
			"zipArchive": "https://github.com/xmlui-org/xmlui-hello-world/releases/download/v1.0.2/xmlui-hello-world.zip"
		},
		{
			"uid": "weather",
			"displayName": "XMLUI Weather Dashboard",
			"author": "xmlui.org",
			"description": "A simple weather dashboard that displays current weather conditions for any city.",
			"zipArchive": "https://github.com/xmlui-org/xmlui-weather/releases/download/v1.0.0/xmlui-weather.zip"
		},
		{
			"uid": "invoice",
			"displayName": "XMLUI Invoice",
			"author": "xmlui.org",
			"description": "A complete business application for invoice management with a local server and database",
			"zipArchive": "https://github.com/xmlui-org/xmlui-invoice/releases/download/v1.0.1/xmlui-invoice.zip"
		}
	]
}`

func HandleNewCmd(opts Options) {
	templates, err := getTemplates()
	if err != nil {
		log.Fatalf("Error loading templates: %v", err)
	}

	var selectedTemplate *Template
	for i := range templates {
		if templates[i].UID == opts.TemplateName {
			selectedTemplate = &templates[i]
			break
		}
	}

	if selectedTemplate == nil {
		log.Fatalf("Unknown template: %s.", opts.TemplateName)
	}

	url := selectedTemplate.ZipArchive
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = selectedTemplate.UID
	}

	fmt.Printf("Downloading template %s...\n", selectedTemplate.DisplayName)
	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Failed to download template: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to download template: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read downloaded template: %v", err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		log.Fatalf("Failed to read zip content: %v", err)
	}

	fmt.Printf("Extracting to %s...\n", outputDir)
	if err := utils.Unzip(zipReader, outputDir); err != nil {
		log.Fatalf("Failed to extract template: %v", err)
	}
}
