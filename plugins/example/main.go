package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	var input struct {
		Config struct {
			RequiredFiles []string `json:"required_files"`
		} `json:"config"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail(err)
	}
	required := input.Config.RequiredFiles
	if len(required) == 0 {
		required = []string{"README.md", "main.go"}
	}
	if len(required) > 100 {
		fail(fmt.Errorf("too many required_files"))
	}
	archive, err := zip.OpenReader("/input/submission.zip")
	if err != nil {
		fail(err)
	}
	defer archive.Close()
	names := make(map[string]bool, len(archive.File))
	for _, f := range archive.File {
		names[strings.TrimPrefix(f.Name, "./")] = true
	}
	found := 0
	for _, name := range required {
		if names[name] {
			found++
		}
	}
	score := 100 * float64(found) / float64(len(required))
	output := map[string]any{"protocol_version": "1", "score": score, "details": map[string]any{"found": found, "required": len(required)}}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		fail(err)
	}
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
