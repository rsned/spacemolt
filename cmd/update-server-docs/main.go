package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	docsDir  = "server_docs"
	skillURL = "https://www.spacemolt.com/skill.md"
	apiURL   = "https://www.spacemolt.com/api.md"
)

type doc struct {
	url      string
	baseName string
}

func main() {
	docs := []doc{
		{url: skillURL, baseName: "skill.md"},
		{url: apiURL, baseName: "api.md"},
	}

	// Ensure docs directory exists
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating docs directory: %v\n", err)
		os.Exit(1)
	}

	dateStamp := time.Now().Format("20060102")

	for _, d := range docs {
		if err := downloadAndLink(d.url, d.baseName, dateStamp); err != nil {
			fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", d.baseName, err)
			os.Exit(1)
		}
	}

	fmt.Println("Documentation updated successfully")
}

func downloadAndLink(url, baseName, dateStamp string) error {
	// Download the file
	content, err := downloadURL(url)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}

	// Create the dated filename
	datedName := fmt.Sprintf("%s.%s.md", strings.TrimSuffix(baseName, ".md"), dateStamp)
	datedPath := filepath.Join(docsDir, datedName)

	// Check if file already exists and has same content
	if existingContent, err := os.ReadFile(datedPath); err == nil {
		if string(existingContent) == content {
			fmt.Printf("%s: already up to date\n", baseName)
			return updateSymlink(datedName, baseName)
		}
	}

	// Write the dated file
	if err := os.WriteFile(datedPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	fmt.Printf("%s: saved as %s\n", baseName, datedName)

	// Update symlink
	return updateSymlink(datedName, baseName)
}

func downloadURL(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP status: %s", resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func updateSymlink(datedName, baseName string) error {
	symlinkPath := filepath.Join(docsDir, baseName)
	targetPath := datedName

	// Remove existing symlink if it exists
	if _, err := os.Lstat(symlinkPath); err == nil {
		if err := os.Remove(symlinkPath); err != nil {
			return fmt.Errorf("removing existing symlink: %w", err)
		}
	}

	// Create new symlink
	if err := os.Symlink(targetPath, symlinkPath); err != nil {
		return fmt.Errorf("creating symlink: %w", err)
	}

	fmt.Printf("%s: symlink -> %s\n", baseName, datedName)
	return nil
}
