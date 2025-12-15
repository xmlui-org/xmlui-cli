package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultPort = "8080"

var staticExtensions = map[string]bool{
	".xmlui": true,
	".xs":    true,
	".html":  true,
	".js":    true,
	".mjs":   true,
	".css":   true,
	".png":   true,
	".jpg":   true,
	".jpeg":  true,
	".gif":   true,
	".svg":   true,
	".ico":   true,
	".webp":  true,
	".woff":  true,
	".woff2": true,
	".ttf":   true,
	".eot":   true,
	".otf":   true,
	".json":  true,
	".xml":   true,
	".txt":   true,
	".map":   true,
	".mp4":   true,
	".webm":  true,
	".mp3":   true,
	".wav":   true,
	".pdf":   true,
	".zip":   true,
	".tar":   true,
	".gz":    true,
}

var logger = log.New(os.Stderr, "", 0)

// Config holds the configuration for the server
type Config struct {
	Dir  string
	Port string
}

// Start launches the SPA server
func Start(cfg Config) error {
	dir := cfg.Dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		dir = wd
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}

	// Determine port and listener
	var listener net.Listener
	port := cfg.Port

	if port != "" {
		listener, err = net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			return fmt.Errorf("failed to listen on port %s: %w", port, err)
		}
	} else {
		listener, err = net.Listen("tcp", "127.0.0.1:"+defaultPort)
		if err != nil {
			// Default port is taken, ask OS for a random port
			listener, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return fmt.Errorf("failed to bind to any port: %w", err)
			}
		}
	}

	// Get the actual port (in case we used 0 or just to be sure)
	addr := listener.Addr().(*net.TCPAddr)
	actualPort := fmt.Sprintf("%d", addr.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		fpath := filepath.Join(absDir, filepath.Clean(r.URL.Path))

		_, err := os.Stat(fpath)
		if os.IsNotExist(err) {
			ext := strings.ToLower(filepath.Ext(r.URL.Path))
			if staticExtensions[ext] {
				http.NotFound(w, r)
				return
			}

			// Fallback to index.html for SPA routing
			http.ServeFile(w, r, filepath.Join(absDir, "index.html"))
			return
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.ServeFile(w, r, fpath)
	})

	url := fmt.Sprintf("http://localhost:%s", actualPort)
	logger.Printf("Serving %s", absDir)
	logger.Printf("Available on: %s", url)

	// Open browser
	go func() {
		time.Sleep(100 * time.Millisecond) // Give the server a split second
		launchBrowser(url)
	}()

	return http.Serve(listener, mux)
}

func launchBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	if err := exec.Command(cmd, args...).Start(); err != nil {
		logger.Printf("Failed to open browser: %v", err)
	}
}
