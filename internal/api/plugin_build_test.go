package api

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func testZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestBuildPluginPackage(t *testing.T) {
	source, err := os.ReadFile("../../plugins/example/main.go")
	if err != nil {
		t.Fatal(err)
	}
	root, err := goModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "uploaded.so")
	if err := buildPluginPackage(context.Background(), root, map[string][]byte{"plugin.go": source}, output); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() == 0 {
		t.Fatalf("compiled plugin missing: %v", err)
	}
}

func TestBuildExamplePlugin(t *testing.T) {
	root, err := goModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "example.so")
	if err := buildExamplePlugin(context.Background(), root, output); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() == 0 {
		t.Fatalf("compiled example plugin missing: %v", err)
	}
	if _, err := os.Stat(output + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary build artifact was not renamed: %v", err)
	}
}

func TestPluginSourceValidation(t *testing.T) {
	if _, err := pluginSourceFiles(".go", []byte("package main\n\n/* #cgo LDFLAGS: -lm */\nimport \"C\"\n\nfunc main() {}\n")); err == nil {
		t.Fatal("cgo source was accepted")
	}
	if _, err := pluginSourceFiles(".go", []byte("package main\n\nimport _ \"github.com/gin-gonic/gin\"\n\nfunc main() {}\n")); err == nil {
		t.Fatal("external dependency was accepted")
	}
	if _, err := pluginSourceFiles(".zip", []byte("not a zip")); err == nil {
		t.Fatal("invalid zip was accepted")
	}
	if _, err := pluginSourceFiles(".zip", testZip(t, map[string]string{"../evil.go": "package main"})); err == nil {
		t.Fatal("path traversal was accepted")
	}
	if _, err := pluginSourceFiles(".zip", testZip(t, map[string]string{"README.md": "no"})); err == nil {
		t.Fatal("archive without Go files was accepted")
	}
	files, err := pluginSourceFiles(".zip", testZip(t, map[string]string{
		"plugin.go":            "package main\nfunc main() {}",
		"README.md":            "ignored",
		"__MACOSX/._plugin.go": "metadata",
	}))
	if err != nil || len(files) != 1 {
		t.Fatalf("non-Go entries should be ignored: files=%v err=%v", files, err)
	}
	files, err = pluginSourceFiles(".zip", testZip(t, map[string]string{"plugin.go": "package main\nfunc main() {}"}))
	if err != nil || len(files) != 1 {
		t.Fatalf("valid zip rejected: files=%v err=%v", files, err)
	}
	files, err = pluginSourceFiles(".zip", testZip(t, map[string]string{"my-plugin/plugin.go": "package main\nfunc main() {}"}))
	if err != nil || len(files) != 1 || files["plugin.go"] == nil {
		t.Fatalf("single top-level directory was not normalized: files=%v err=%v", files, err)
	}
	if _, err := pluginSourceFiles(".zip", testZip(t, map[string]string{
		"one/plugin.go": "package main\nfunc main() {}",
		"two/plugin.go": "package main\nfunc main() {}",
	})); err == nil {
		t.Fatal("multiple packages were accepted")
	}
}
