package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const (
	maxPluginRequestBytes    = 13 << 20
	maxPluginSourceBytes     = 12 << 20
	maxPluginUnpackedBytes   = 16 << 20
	maxPluginSourceFileBytes = 4 << 20
	maxPluginSourceFiles     = 100
	maxPluginBuildOutput     = 128 << 20
	pluginBuildTimeout       = 90 * time.Second

	examplePluginSourceRel = "plugins/example/main.go"
	examplePluginBinaryRel = "plugins/example/example.so"
	pluginAPIImportPath    = "github.com/Wh1teJ0ker/birkenwald-recruit/pluginapi"
)

var (
	pluginBuildMu       sync.Mutex
	cgoDirectivePattern = regexp.MustCompile(`(?m)^\s*//\s*#cgo\b`)
	unsafePluginName    = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

// uploadPlugin compiles an uploaded single Go file or a ZIP of Go files into a
// worker-loadable plugin. Compilation runs as a child process with the API
// process permissions; only trusted administrators should be allowed to call
// this endpoint.
func (s *Server) uploadPlugin(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPluginRequestBytes)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		fail(c, 413, "upload is malformed or too large")
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	name := strings.TrimSpace(c.PostForm("name"))
	version := strings.TrimSpace(c.PostForm("version"))
	if name == "" || version == "" ||
		utf8.RuneCountInString(name) > 80 || utf8.RuneCountInString(version) > 40 ||
		strings.ContainsAny(name, " \t\r\n") || strings.ContainsAny(version, " \t\r\n") {
		fail(c, 422, "name and version are required")
		return
	}
	schema := []byte(strings.TrimSpace(c.PostForm("config_schema")))
	if len(schema) == 0 {
		schema = []byte("{}")
	}
	if !validPluginSchema(schema) {
		fail(c, 422, "config_schema must be an object")
		return
	}
	enabled := true
	if raw := strings.TrimSpace(c.PostForm("enabled")); raw != "" {
		switch strings.ToLower(raw) {
		case "true":
		case "false":
			enabled = false
		default:
			fail(c, 422, "enabled must be a boolean")
			return
		}
	}

	header, err := c.FormFile("file")
	if err != nil {
		fail(c, 422, "file is required")
		return
	}
	if header.Size <= 0 || header.Size > maxPluginSourceBytes {
		fail(c, 413, "plugin source is too large")
		return
	}
	extension := strings.ToLower(filepath.Ext(header.Filename))
	if extension != ".go" && extension != ".zip" {
		fail(c, 422, "file must be a .go source file or .zip archive")
		return
	}
	file, err := header.Open()
	if err != nil {
		fail(c, 422, "cannot read uploaded file")
		return
	}
	defer file.Close()
	source, err := io.ReadAll(io.LimitReader(file, maxPluginSourceBytes+1))
	if err != nil {
		fail(c, 422, "cannot read uploaded file")
		return
	}
	if len(source) == 0 || len(source) > maxPluginSourceBytes {
		fail(c, 413, "plugin source is too large")
		return
	}
	files, err := pluginSourceFiles(extension, source)
	if err != nil {
		fail(c, 422, err.Error())
		return
	}

	ctx := c.Request.Context()
	var exists bool
	if err := s.DB.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM plugin_versions WHERE name=$1 AND version=$2)",
		name, version).Scan(&exists); err != nil {
		fail(c, 500, "create plugin failed")
		return
	}
	if exists {
		fail(c, 409, "plugin version already exists")
		return
	}

	root, err := goModuleRoot()
	if err != nil {
		fail(c, 503, "plugin compilation requires the API to run from the source repository")
		return
	}
	if _, err := exec.LookPath("go"); err != nil {
		fail(c, 503, "plugin compilation requires the Go toolchain on the API host")
		return
	}
	storageDir, err := pluginStorageDir(s.Config.UploadDir)
	if err != nil {
		fail(c, 500, "prepare plugin storage failed")
		return
	}
	suffix, err := randomSuffix()
	if err != nil {
		fail(c, 500, "create plugin failed")
		return
	}
	output := filepath.Join(storageDir, safePluginFileName(name, version)+"-"+suffix+".so")

	pluginBuildMu.Lock()
	err = buildPluginPackage(ctx, root, files, output)
	pluginBuildMu.Unlock()
	if err != nil {
		_ = os.Remove(output)
		fail(c, 422, err.Error())
		return
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() == 0 || info.Size() > maxPluginBuildOutput {
		_ = os.Remove(output)
		fail(c, 422, "compiled plugin is missing or too large")
		return
	}
	if err := os.Chmod(output, 0o600); err != nil {
		_ = os.Remove(output)
		fail(c, 500, "create plugin failed")
		return
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		_ = os.Remove(output)
		fail(c, 500, "create plugin failed")
		return
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO plugin_versions(name,version,library_path,config_schema,enabled)
        VALUES($1,$2,$3,$4,$5) RETURNING id`, name, version, output, schema, enabled).Scan(&id)
	if isUniqueViolation(err) {
		_ = os.Remove(output)
		fail(c, 409, "plugin version already exists")
		return
	}
	if err != nil {
		_ = os.Remove(output)
		fail(c, 500, "create plugin failed")
		return
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,'plugin.compile','plugin_version',$2)", currentUser(c).ID, id); err != nil {
		_ = os.Remove(output)
		fail(c, 500, "create plugin failed")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		_ = os.Remove(output)
		fail(c, 500, "create plugin failed")
		return
	}
	c.JSON(201, gin.H{
		"id": id, "name": name, "version": version,
		"library_path": output, "enabled": enabled,
	})
}

func pluginSourceFiles(extension string, source []byte) (map[string][]byte, error) {
	if extension == ".go" {
		if err := validatePluginSource("plugin.go", source); err != nil {
			return nil, err
		}
		return map[string][]byte{"plugin.go": source}, nil
	}
	reader, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		return nil, errors.New("invalid zip archive")
	}
	files := make(map[string][]byte)
	total := 0
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		raw := entry.Name
		if strings.Contains(raw, `\`) {
			return nil, fmt.Errorf("invalid path in zip: %s", raw)
		}
		name := path.Clean(strings.TrimPrefix(raw, "./"))
		if name == "." || name == ".." || path.IsAbs(name) || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("invalid path in zip: %s", raw)
		}
		base := path.Base(name)
		if strings.HasPrefix(name, "__MACOSX/") || strings.HasPrefix(base, "._") || base == ".DS_Store" {
			continue
		}
		if strings.ToLower(path.Ext(name)) != ".go" {
			continue
		}
		if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
			return nil, fmt.Errorf("hidden or ignored Go file in zip: %s", raw)
		}
		if len(files) >= maxPluginSourceFiles {
			return nil, fmt.Errorf("zip contains more than %d files", maxPluginSourceFiles)
		}
		if _, duplicate := files[name]; duplicate {
			return nil, fmt.Errorf("duplicate file in zip: %s", name)
		}
		if entry.UncompressedSize64 > maxPluginSourceFileBytes {
			return nil, fmt.Errorf("file is too large: %s", raw)
		}
		total += int(entry.UncompressedSize64)
		if total > maxPluginUnpackedBytes {
			return nil, errors.New("unpacked plugin source is too large")
		}
		stream, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("cannot read %s", raw)
		}
		content, readErr := io.ReadAll(io.LimitReader(stream, maxPluginSourceFileBytes+1))
		if closeErr := stream.Close(); readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("cannot read %s", raw)
		}
		if len(content) > maxPluginSourceFileBytes {
			return nil, fmt.Errorf("file is too large: %s", raw)
		}
		if err := validatePluginSource(name, content); err != nil {
			return nil, err
		}
		files[name] = content
	}
	if len(files) == 0 {
		return nil, errors.New("zip contains no .go files")
	}
	return normalizePluginZipPaths(files)
}

func normalizePluginZipPaths(files map[string][]byte) (map[string][]byte, error) {
	prefix := ""
	hasRootFiles := false
	for name := range files {
		if !strings.Contains(name, "/") {
			hasRootFiles = true
			continue
		}
		first := strings.SplitN(name, "/", 2)[0]
		if prefix == "" {
			prefix = first
		} else if prefix != first {
			return nil, errors.New("zip must contain a single plugin package")
		}
	}
	if prefix != "" && !hasRootFiles {
		normalized := make(map[string][]byte, len(files))
		for name, content := range files {
			normalized[strings.TrimPrefix(name, prefix+"/")] = content
		}
		files = normalized
	}
	for name := range files {
		if strings.Contains(name, "/") {
			return nil, errors.New("zip must contain a single plugin package at its root")
		}
	}
	return files, nil
}

func validatePluginSource(name string, source []byte) error {
	if len(source) == 0 {
		return fmt.Errorf("%s is empty", name)
	}
	if len(source) > maxPluginSourceFileBytes {
		return fmt.Errorf("%s is too large", name)
	}
	if cgoDirectivePattern.Match(source) {
		return fmt.Errorf("%s uses cgo, which is not allowed", name)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ImportsOnly)
	if err == nil {
		for _, imported := range parsed.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return fmt.Errorf("%s has an invalid import", name)
			}
			if importPath == "C" {
				return fmt.Errorf("%s uses cgo, which is not allowed", name)
			}
			first, _, _ := strings.Cut(importPath, "/")
			if importPath != pluginAPIImportPath && strings.Contains(first, ".") {
				return fmt.Errorf("%s imports %s; only the standard library and pluginapi are allowed", name, importPath)
			}
		}
	}
	return nil
}

func buildPluginPackage(parent context.Context, moduleRoot string, files map[string][]byte, output string) error {
	buildRoot := filepath.Join(moduleRoot, "plugins")
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		return fmt.Errorf("prepare plugin build directory: %w", err)
	}
	buildDir, err := os.MkdirTemp(buildRoot, ".upload-*")
	if err != nil {
		return fmt.Errorf("prepare plugin build directory: %w", err)
	}
	defer os.RemoveAll(buildDir)
	for name, source := range files {
		destination := filepath.Join(buildDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return fmt.Errorf("prepare plugin source: %w", err)
		}
		if err := os.WriteFile(destination, source, 0o600); err != nil {
			return fmt.Errorf("prepare plugin source: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(parent, pluginBuildTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-buildmode=plugin", "-trimpath", "-o", output, ".")
	command.Dir = buildDir
	command.Env = pluginBuildEnv()
	result, err := command.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("plugin compilation timed out")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return errors.New("plugin compilation was canceled")
	}
	if err != nil {
		message := strings.TrimSpace(string(result))
		message = strings.ReplaceAll(message, buildDir, "<upload>")
		if message == "" {
			message = err.Error()
		}
		if len(message) > 4000 {
			message = message[:4000] + "..."
		}
		return fmt.Errorf("compile failed: %s", message)
	}
	return nil
}

func pluginBuildEnv() []string {
	drop := map[string]bool{
		"CGO_ENABLED": true, "GOPROXY": true, "GOTOOLCHAIN": true,
		"GOFLAGS": true, "GOWORK": true,
	}
	env := make([]string, 0, len(os.Environ())+5)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !drop[key] {
			env = append(env, item)
		}
	}
	return append(env,
		"CGO_ENABLED=1",
		"GOPROXY=off",
		"GOTOOLCHAIN=local",
		"GOFLAGS=-mod=readonly",
		"GOWORK=off",
	)
}

func goModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "pluginapi", "pluginapi.go")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("repository root with pluginapi not found")
		}
		dir = parent
	}
}

func pluginStorageDir(uploadDir string) (string, error) {
	dir := filepath.Join(uploadDir, "plugins")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func safePluginFileName(name, version string) string {
	value := strings.Trim(unsafePluginName.ReplaceAllString(name+"-"+version, "_"), "._-")
	if value == "" {
		value = "plugin"
	}
	if len(value) > 100 {
		value = value[:100]
	}
	return value
}

func randomSuffix() (string, error) {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func (s *Server) downloadExamplePluginPackage(c *gin.Context) {
	root, err := goModuleRoot()
	if err != nil {
		fail(c, 503, "example plugin package is unavailable")
		return
	}
	files := []struct {
		name string
		path string
	}{
		{"example-plugin/main.go", filepath.Join(root, examplePluginSourceRel)},
		{"example-plugin/README.md", filepath.Join(root, "plugins/example/README.md")},
		{"example-plugin/AGENTS.md", filepath.Join(root, "plugins/example/AGENTS.md")},
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, file := range files {
		content, err := os.ReadFile(file.path)
		if err != nil {
			_ = writer.Close()
			fail(c, 500, "example plugin package is incomplete")
			return
		}
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		header.SetMode(0o644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			fail(c, 500, "build example plugin package failed")
			return
		}
		if _, err := entry.Write(content); err != nil {
			_ = writer.Close()
			fail(c, 500, "build example plugin package failed")
			return
		}
	}
	if err := writer.Close(); err != nil {
		fail(c, 500, "build example plugin package failed")
		return
	}
	c.Header("Content-Disposition", `attachment; filename="example-plugin.zip"`)
	c.Data(http.StatusOK, "application/zip", buffer.Bytes())
}

func (s *Server) downloadExamplePluginBinary(c *gin.Context) {
	root, err := goModuleRoot()
	if err != nil {
		fail(c, 503, "example plugin is unavailable")
		return
	}
	binary := filepath.Join(root, examplePluginBinaryRel)
	source := filepath.Join(root, examplePluginSourceRel)
	sourceInfo, sourceErr := os.Stat(source)
	binaryInfo, binaryErr := os.Stat(binary)
	if binaryErr != nil || (sourceErr == nil && sourceInfo.ModTime().After(binaryInfo.ModTime())) {
		pluginBuildMu.Lock()
		err = buildExamplePlugin(c.Request.Context(), root, binary)
		pluginBuildMu.Unlock()
		if err != nil {
			fail(c, 500, err.Error())
			return
		}
	}
	c.FileAttachment(binary, "example.so")
}

func buildExamplePlugin(parent context.Context, root, output string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return fmt.Errorf("prepare plugin build directory: %w", err)
	}
	temporary := output + ".tmp"
	_ = os.Remove(temporary)
	ctx, cancel := context.WithTimeout(parent, pluginBuildTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-buildmode=plugin", "-trimpath", "-o", temporary, "./plugins/example")
	command.Dir = root
	command.Env = pluginBuildEnv()
	result, err := command.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		_ = os.Remove(temporary)
		return errors.New("example plugin compilation timed out")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		_ = os.Remove(temporary)
		return errors.New("example plugin compilation was canceled")
	}
	if err != nil {
		_ = os.Remove(temporary)
		message := strings.TrimSpace(string(result))
		if message == "" {
			message = err.Error()
		}
		if len(message) > 4000 {
			message = message[:4000] + "..."
		}
		return fmt.Errorf("compile example plugin failed: %s", message)
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, output); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
