package worker

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidateResult(t *testing.T) {
	tests := []struct {
		name    string
		result  Result
		wantErr bool
	}{
		{"zero is a valid score", Result{ProtocolVersion: "1", Score: 0}, false},
		{"hundred is a valid score", Result{ProtocolVersion: "1", Score: 100, Details: json.RawMessage(`{"passed":true}`)}, false},
		{"wrong protocol", Result{ProtocolVersion: "2", Score: 90}, true},
		{"negative", Result{ProtocolVersion: "1", Score: -1}, true},
		{"over maximum", Result{ProtocolVersion: "1", Score: 101}, true},
		{"NaN", Result{ProtocolVersion: "1", Score: math.NaN()}, true},
		{"infinite", Result{ProtocolVersion: "1", Score: math.Inf(1)}, true},
		{"invalid details", Result{ProtocolVersion: "1", Score: 90, Details: json.RawMessage("{")}, true},
		{"oversized details", Result{ProtocolVersion: "1", Score: 90, Details: json.RawMessage(strings.Repeat(" ", 32769))}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateResult(tc.result)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateResult() error = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestRunGoPluginExample(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Go plugins are only supported on Linux")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(t.TempDir(), "example.so")
	args := []string{"build", "-buildmode=plugin", "-o", pluginPath}
	if raceEnabled {
		args = append(args, "-race")
	}
	args = append(args, "./plugins/example")
	build := exec.Command("go", args...)
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=1")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build example plugin: %v\n%s", err, output)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"README.md", "main.go"} {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("test")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	submissionPath := filepath.Join(t.TempDir(), "submission.zip")
	if err := os.WriteFile(submissionPath, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := &Worker{}
	got, err := w.RunGoPlugin(ctx, Job{
		ID: 1, SubmissionID: 2, TaskID: 3, LibraryPath: pluginPath, PackagePath: submissionPath,
		Config: json.RawMessage(`{"required_files":["README.md","main.go"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateResult(got); err != nil || got.Score != 100 {
		t.Fatalf("unexpected plugin result: %+v, %v", got, err)
	}
}
