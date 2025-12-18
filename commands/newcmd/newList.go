package newcmd

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

func HandleListCmd() {
	templates, err := getTemplates()
	if err != nil {
		log.Fatalf("Error while querrying templates: %v", err)
	}

	for _, t := range templates {
		fmt.Printf("%s: %s\n\n", t.UID, t.Description)
	}
}

const templatesApiUrl = "https://xmlui.org/api/v1/templates/"

func getTemplates() ([]Template, error) {
	// Try fetching from API
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(templatesApiUrl)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		contentType := resp.Header.Get("Content-Type")
		if contentType != "" && contentType != "application/json" {
			return nil, fmt.Errorf("Expected JSON format, but content-type is: %v", contentType)
		}
		defer resp.Body.Close()
		var registry TemplateRegistry
		if err := json.NewDecoder(resp.Body).Decode(&registry); err != nil {
			return nil, err
		}
		return registry.Templates, nil
	} else {
		return nil, fmt.Errorf("Got a non Ok response status code while querrying templates: %v", resp.Status)
	}

}
