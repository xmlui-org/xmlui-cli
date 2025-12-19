package newcmd

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

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
		utils.ConsoleLogger.Fatalf("Error loading apps: %v", err)
	}

	var selectedTemplate *Template
	for i := range templates {
		if templates[i].UID == opts.TemplateName {
			selectedTemplate = &templates[i]
			break
		}
	}

	if selectedTemplate == nil {
		utils.ConsoleLogger.Fatalf("Unknown app: %s.", opts.TemplateName)
	}

	url := selectedTemplate.ZipArchive
	outputDir := opts.OutputDir

	if outputDir != "" {
		if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
			utils.ConsoleLogger.Fatalf("Error: Specified output directory already exists: %s", outputDir)
		}
	} else {
		outputDir = selectedTemplate.UID
		if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
			destDir, err := os.Getwd()
			if err != nil {
				utils.ConsoleLogger.Fatalf("Failed to get current working directory: %v", err)
			}

			entries, err := os.ReadDir(destDir)
			if err != nil {
				utils.ConsoleLogger.Fatalf("Failed to read directory %s: %v", destDir, err)
			}

			maxNum := 0
			prefix := outputDir + "-"

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
			outputDir = fmt.Sprintf("%s-%d", outputDir, maxNum+1)
		}
	}

	utils.ConsoleLogger.Printf("Downloading %s (%s)...\n", selectedTemplate.UID, selectedTemplate.DisplayName)
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
