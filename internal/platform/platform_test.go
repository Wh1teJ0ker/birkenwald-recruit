package platform

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func uploadHeader(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	f, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "/files", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = req.MultipartForm.RemoveAll() })
	_, header, err := req.FormFile("file")
	if err != nil {
		t.Fatal(err)
	}
	return header
}

func zipContent(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(f, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSaveUploadValidFiles(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		content []byte
	}{
		{"resume", "resume", []byte("%PDF-1.4\n%%EOF")},
		{"package", "package", zipContent(t, map[string]string{"README.md": "hello"})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			header := uploadHeader(t, "test-file", tc.content)
			header.Filename = "../../test-file"
			saved, err := SaveUpload(dir, tc.kind, header)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Name != "test-file" || saved.Size != int64(len(tc.content)) || len(saved.Key) != 64 {
				t.Fatalf("unexpected metadata: %+v", saved)
			}
			sum := sha256.Sum256(tc.content)
			if saved.SHA256 != hex.EncodeToString(sum[:]) {
				t.Fatalf("SHA-256 mismatch: %s", saved.SHA256)
			}
			got, err := os.ReadFile(saved.Path)
			if err != nil || !bytes.Equal(got, tc.content) {
				t.Fatalf("saved contents differ: %v", err)
			}
			info, err := os.Stat(saved.Path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("file is not private: %v, %v", info, err)
			}
		})
	}
}

func TestSaveUploadRejectsUnsafeFiles(t *testing.T) {
	traversal := zipContent(t, map[string]string{"../outside.txt": "no"})
	zipBomb := zipContent(t, map[string]string{"large.txt": strings.Repeat("A", 2<<20)})
	tests := []struct {
		name    string
		kind    string
		content []byte
	}{
		{"fake PDF", "resume", []byte("not a pdf")},
		{"fake ZIP with local header", "package", []byte{'P', 'K', 3, 4, 'x'}},
		{"traversal", "package", traversal},
		{"compression bomb", "package", zipBomb},
		{"empty", "resume", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := SaveUpload(dir, tc.kind, uploadHeader(t, "file", tc.content))
			if err == nil {
				t.Fatal("unsafe upload was accepted")
			}
			entries, err := os.ReadDir(dir)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("rejected upload left %d files", len(entries))
			}
		})
	}
}

func TestSaveUploadRejectsDeclaredOversize(t *testing.T) {
	header := uploadHeader(t, "resume.pdf", []byte("%PDF-1.4\n%%EOF"))
	header.Size = 10<<20 + 1
	dir := t.TempDir()
	if _, err := SaveUpload(dir, "resume", header); err == nil {
		t.Fatal("oversized file was accepted")
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "*")); err != nil || len(matches) != 0 {
		t.Fatalf("oversized upload left files: %v, %v", matches, err)
	}
}
