package newcmd

import (
	"archive/zip"
	"bytes"
	"io"
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

func HandleNewCmd(opts Options) {
	templates, err := getTemplates()
	if err != nil {
		utils.ConsoleLogger.Fatalf("Error loading templates: %v", err)
	}

	var selectedTemplate *Template
	for i := range templates {
		if templates[i].UID == opts.TemplateName {
			selectedTemplate = &templates[i]
			break
		}
	}

	if selectedTemplate == nil {
		utils.ConsoleLogger.Fatalf("Unknown template: %s.", opts.TemplateName)
	}

	url := selectedTemplate.ZipArchive
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = selectedTemplate.UID
	}

	utils.ConsoleLogger.Printf("Downloading %s (%s)...\n", outputDir, selectedTemplate.DisplayName)
	resp, err := http.Get(url)
	if err != nil {
		utils.ConsoleLogger.Fatalf("Failed to download from %s\nError: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		utils.ConsoleLogger.Fatalf("Failed to download from %s\nError: %s", url, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		utils.ConsoleLogger.Fatalf("Failed to read downloaded app: %v", err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		utils.ConsoleLogger.Fatalf("Failed to read zip content: %v", err)
	}

	utils.ConsoleLogger.Printf("Extracting to %s...\n", outputDir)
	if err := utils.Unzip(zipReader, outputDir); err != nil {
		utils.ConsoleLogger.Fatalf("Failed to extract: %v", err)
	} else {
		utils.ConsoleLogger.Printf("To run the app, visit %s and use `xmlui run`\n", outputDir)
	}
}
