package worker

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
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

func TestLimitedBufferCapsOutput(t *testing.T) {
	b := &limitedBuffer{limit: 4}
	if n, err := b.Write([]byte("123456")); n != 6 || err != nil || b.String() != "1234" {
		t.Fatalf("unexpected bounded write: n=%d err=%v value=%q", n, err, b.String())
	}
	if n, err := b.Write([]byte("789")); n != 3 || err != nil || b.String() != "1234" {
		t.Fatalf("unexpected second write: n=%d err=%v value=%q", n, err, b.String())
	}
}

func TestRunDockerExamplePlugin(t *testing.T) {
	image := os.Getenv("TEST_DOCKER_PLUGIN_IMAGE")
	if image == "" {
		t.Skip("set TEST_DOCKER_PLUGIN_IMAGE after building plugins/example image")
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
	path := filepath.Join(t.TempDir(), "submission.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := &Worker{}
	got, err := w.RunDocker(ctx, Job{ID: 1, SubmissionID: 2, TaskID: 3, Image: image, PackagePath: path,
		Config: json.RawMessage(`{"required_files":["README.md","main.go"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateResult(got); err != nil || got.Score != 100 {
		t.Fatalf("unexpected plugin result: %+v, %v", got, err)
	}
}
