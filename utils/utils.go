package utils

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// consoleLogger is a simple logger that writes to stderr without timestamps or prefixes.
type consoleLogger struct{}

var (
	// ConsoleLogger is the global instance of consoleLogger for use throughout the application.
	ConsoleLogger = &consoleLogger{}
)

// Printf formats and prints to stderr.
func (l *consoleLogger) Printf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}

// Println prints to stderr with a newline.
func (l *consoleLogger) Println(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
}

// Print prints to stderr without a newline.
func (l *consoleLogger) Print(args ...any) {
	fmt.Fprint(os.Stderr, args...)
}

// Fatalf prints an error message to stderr and exits with status 1.
func (l *consoleLogger) Fatalf(format string, args ...any) {
	l.Printf(format, args...)
	os.Exit(1)
}

// Fatal prints an error message to stderr and exits with status 1.
func (l *consoleLogger) Fatal(args ...any) {
	l.Println(args...)
	os.Exit(1)
}

// Unzip extracts a zip archive to the destination directory.
// It takes a *zip.Reader which allows extracting from both files (via zip.NewReader)
// and memory/streams (via bytes.NewReader + zip.NewReader).
func Unzip(r *zip.Reader, dest string) error {
	if err := os.MkdirAll(dest, os.ModePerm); err != nil {
		return fmt.Errorf("Error creating file %s", dest)
	}

	extractPrefix := ""
	// Check if the zip has a single top-level directory
	if len(r.File) > 0 {

		first := r.File[0].Name
		parts := strings.Split(first, "/")

		candidate := parts[0]
		allShareFirstPathPart := true
		aTopLvlDirExists := false

		for _, f := range r.File {
			currentParts := strings.Split(f.Name, "/")

			if currentParts[0] != candidate {
				allShareFirstPathPart = false
				break
			}

			// If we have "candidate/something" or the entry "candidate/" is explicitly a dir
			if len(currentParts) > 1 || f.FileInfo().IsDir() {
				aTopLvlDirExists = true
			}
		}

		if allShareFirstPathPart && aTopLvlDirExists {
			extractPrefix = candidate + "/"
		}
	}
	for _, f := range r.File {
		name := f.Name

		if extractPrefix != "" {
			if after, ok := strings.CutPrefix(name, extractPrefix); ok {
				if after == "" {
					continue
				}
				name = after
			} else {
				// Handle exact match with the directory name
				// in case it doesn't have a trailing slash
				// (e.g. "dir" matching prefix "dir/")
				// We must either cut a prefix or skip the root dir, since we determined there's for sure a single root dir
				continue
			}
		}

		fpath := filepath.Join(dest, name)

		// Check for Zip Slip vulnerability
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}
