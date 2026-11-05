package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wh1teJ0ker/birkenwald-recruit/pluginapi"
)

// main is required so the package can also be type-checked by `go build ./...`.
// It is not called when the package is loaded with -buildmode=plugin.
func main() {}

// Evaluate is loaded by the worker through plugin.Open("...").Lookup("Evaluate").
func Evaluate(ctx context.Context, req pluginapi.Request) (pluginapi.Result, error) {
	if err := ctx.Err(); err != nil {
		return pluginapi.Result{}, err
	}
	var config struct {
		RequiredFiles []string `json:"required_files"`
	}
	if len(req.Config) > 0 {
		if err := json.Unmarshal(req.Config, &config); err != nil {
			return pluginapi.Result{}, fmt.Errorf("parse config: %w", err)
		}
	}
	required := config.RequiredFiles
	if len(required) == 0 {
		required = []string{"README.md", "main.go"}
	}
	if len(required) > 100 {
		return pluginapi.Result{}, fmt.Errorf("too many required_files")
	}
	archive, err := zip.OpenReader(req.SubmissionPath)
	if err != nil {
		return pluginapi.Result{}, err
	}
	defer archive.Close()
	names := make(map[string]bool, len(archive.File))
	for _, file := range archive.File {
		names[strings.TrimPrefix(file.Name, "./")] = true
	}
	found := 0
	for _, name := range required {
		if names[name] {
			found++
		}
	}
	if err := ctx.Err(); err != nil {
		return pluginapi.Result{}, err
	}
	details, err := json.Marshal(map[string]any{"found": found, "required": len(required)})
	if err != nil {
		return pluginapi.Result{}, err
	}
	return pluginapi.Result{
		ProtocolVersion: pluginapi.ProtocolVersion,
		Score:           100 * float64(found) / float64(len(required)),
		Details:         details,
	}, nil
}
