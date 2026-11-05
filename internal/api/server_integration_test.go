package api_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/api"
	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/platform"
	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
)

type apiResponse struct {
	status int
	body   any
	err    error
}

type testClient struct {
	base string
	http *http.Client
	csrf string
}

func (c *testClient) do(method, path, contentType string, body []byte, extra map[string]string) apiResponse {
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return apiResponse{err: err}
	}
	req.Header.Set("Origin", c.base)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return apiResponse{err: err}
	}
	defer resp.Body.Close()
	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return apiResponse{status: resp.StatusCode, err: err}
	}
	return apiResponse{status: resp.StatusCode, body: result}
}

func (c *testClient) json(method, path string, body any, extra map[string]string) apiResponse {
	var encoded []byte
	if body != nil {
		encoded, _ = json.Marshal(body)
	}
	return c.do(method, path, "application/json", encoded, extra)
}

func (c *testClient) upload(kind, name string, data []byte) apiResponse {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("kind", kind)
	f, err := w.CreateFormFile("file", name)
	if err != nil {
		return apiResponse{err: err}
	}
	if _, err := f.Write(data); err != nil {
		return apiResponse{err: err}
	}
	if err := w.Close(); err != nil {
		return apiResponse{err: err}
	}
	return c.do(http.MethodPost, "/api/v1/files", w.FormDataContentType(), body.Bytes(), nil)
}

func requireStatus(t *testing.T, got apiResponse, want int) any {
	t.Helper()
	if got.err != nil || got.status != want {
		t.Fatalf("HTTP status=%d body=%v error=%v; want %d", got.status, got.body, got.err, want)
	}
	return got.body
}

func object(t *testing.T, data any) map[string]any {
	t.Helper()
	v, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("wanted JSON object, got %T: %v", data, data)
	}
	return v
}

func number(t *testing.T, value any) int64 {
	t.Helper()
	n, ok := value.(float64)
	if !ok {
		t.Fatalf("wanted JSON number, got %T: %v", value, value)
	}
	return int64(n)
}

func clientFor(t *testing.T, server *httptest.Server) *testClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &testClient{base: server.URL, http: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

func (c *testClient) loadCSRF(t *testing.T) {
	t.Helper()
	me := object(t, requireStatus(t, c.json(http.MethodGet, "/api/v1/me", nil, nil), 200))
	token, ok := me["csrf_token"].(string)
	if !ok || token == "" {
		t.Fatal("missing CSRF token")
	}
	c.csrf = token
}

func testServer(t *testing.T) (*pgxpool.Pool, *httptest.Server, string) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to an isolated PostgreSQL test database")
	}
	ctx := context.Background()
	root, err := platform.OpenDB(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "bw_test_" + hex.EncodeToString(random)
	if _, err := root.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := platform.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	adminEmail := "admin-" + schema + "@example.org"
	if err := platform.SeedAdmin(ctx, db, adminEmail, "test-admin-password-123"); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer((&api.Server{DB: db, Config: platform.Config{UploadDir: t.TempDir(), PluginTimeout: 10 * time.Second}}).Router())
	t.Cleanup(s.Close)
	return db, s, adminEmail
}

func createZip(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, name := range []string{"README.md", "main.go"} {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(f, "content")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestMVPFlowConcurrencyAndLeaseFencing(t *testing.T) {
	db, server, adminEmail := testServer(t)
	admin := clientFor(t, server)
	requireStatus(t, admin.json(http.MethodPost, "/api/v1/auth/login", map[string]any{"email": adminEmail, "password": "test-admin-password-123"}, nil), 200)
	admin.loadCSRF(t)
	now := time.Now().UTC()
	base := "/api/v1"
	campaign := object(t, requireStatus(t, admin.json(http.MethodPost, base+"/admin/campaigns", map[string]any{
		"name": "MVP integration", "opens_at": now.Add(-time.Minute), "closes_at": now.Add(2 * time.Hour), "open": true,
	}, nil), 201))
	campaignID := number(t, campaign["id"])
	createTask := func(title string) int64 {
		task := object(t, requireStatus(t, admin.json(http.MethodPost, base+"/admin/tasks", map[string]any{
			"campaign_id": campaignID, "title": title, "description": "test", "deadline_at": now.Add(time.Hour),
			"capacity": 1, "plugin_version_id": 1, "plugin_config": map[string]any{"required_files": []string{"README.md", "main.go"}}, "publish": true,
		}, nil), 201))
		return number(t, task["id"])
	}
	taskID, otherTaskID := createTask("Go"), createTask("React")
	students := []*testClient{clientFor(t, server), clientFor(t, server)}
	for i, student := range students {
		requireStatus(t, student.json(http.MethodPost, base+"/auth/register", map[string]any{
			"email": fmt.Sprintf("student%d-%d@example.org", i, time.Now().UnixNano()), "name": "Student", "password": "test-student-password-123",
		}, nil), 201)
		student.loadCSRF(t)
		requireStatus(t, student.json(http.MethodGet, base+"/admin/submissions", nil, nil), 403)
	}
	selectionPath := fmt.Sprintf("%s/campaigns/%d/selections", base, campaignID)
	results := make([]apiResponse, 2)
	var group sync.WaitGroup
	for i := range students {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			results[i] = students[i].json(http.MethodPost, selectionPath, map[string]any{"task_id": taskID}, nil)
		}(i)
	}
	group.Wait()
	statuses := []int{results[0].status, results[1].status}
	sort.Ints(statuses)
	if statuses[0] != 201 || statuses[1] != 409 || results[0].err != nil || results[1].err != nil {
		t.Fatalf("capacity race: %+v", results)
	}
	winner := 0
	if results[1].status == 201 {
		winner = 1
	}
	student := students[winner]
	selectionID := number(t, object(t, results[winner].body)["id"])
	requireStatus(t, students[1-winner].json(http.MethodPost, selectionPath, map[string]any{"task_id": otherTaskID}, nil), 201)
	again := object(t, requireStatus(t, student.json(http.MethodPost, selectionPath, map[string]any{"task_id": otherTaskID}, nil), 200))
	if number(t, again["task_id"]) != taskID {
		t.Fatalf("student changed task: %v", again)
	}
	var count, selectedCount int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM selections WHERE campaign_id=$1", campaignID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("selection count=%d, err=%v", count, err)
	}
	if err := db.QueryRow(context.Background(), "SELECT selected_count FROM tasks WHERE id=$1", taskID).Scan(&selectedCount); err != nil || selectedCount != 1 {
		t.Fatalf("capacity count=%d, err=%v", selectedCount, err)
	}
	thirdStudent := clientFor(t, server)
	thirdRegistration := object(t, requireStatus(t, thirdStudent.json(http.MethodPost, base+"/auth/register", map[string]any{
		"email": fmt.Sprintf("student3-%d@example.org", time.Now().UnixNano()), "name": "Third Student", "password": "test-student-password-123",
	}, nil), 201))
	thirdStudent.loadCSRF(t)
	moreTasks := []int64{createTask("Design"), createTask("Data")}
	var oneStudentResults [2]apiResponse
	for i := range moreTasks {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			oneStudentResults[i] = thirdStudent.json(http.MethodPost, selectionPath, map[string]any{"task_id": moreTasks[i]}, nil)
		}(i)
	}
	group.Wait()
	created := 0
	for _, result := range oneStudentResults {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.status == 201 {
			created++
		} else if result.status != 200 && result.status != 409 {
			t.Fatalf("unexpected concurrent selection result: %+v", result)
		}
	}
	if created != 1 {
		t.Fatalf("same student created %d selections: %+v", created, oneStudentResults)
	}
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM selections WHERE campaign_id=$1 AND student_id=$2", campaignID, number(t, thirdRegistration["id"])).Scan(&count); err != nil || count != 1 {
		t.Fatalf("same-student selection count=%d, err=%v", count, err)
	}
	requireStatus(t, student.upload("package", "bad.zip", []byte{'P', 'K', 3, 4, 'x'}), 422)
	resume := object(t, requireStatus(t, student.upload("resume", "resume.pdf", []byte("%PDF-1.4\n%%EOF")), 201))
	pack := object(t, requireStatus(t, student.upload("package", "work.zip", createZip(t)), 201))
	otherStudent := students[1-winner]
	privateFilePath := fmt.Sprintf("%s/files/%d/download", base, number(t, resume["id"]))
	requireStatus(t, otherStudent.json(http.MethodGet, privateFilePath, nil, nil), 403)
	requireStatus(t, otherStudent.do(http.MethodPost, selectionPath, "application/json", []byte(fmt.Sprintf("{\"task_id\":%d}", taskID)), map[string]string{"Origin": "https://attacker.example"}), 403)
	payload := map[string]any{"resume_file_id": number(t, resume["id"]), "package_file_id": number(t, pack["id"])}
	submitPath := fmt.Sprintf("%s/selections/%d/submissions", base, selectionID)
	key := "integration-key-one"
	submits := make([]apiResponse, 2)
	for i := range submits {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			submits[i] = student.json(http.MethodPost, submitPath, payload, map[string]string{"Idempotency-Key": key})
		}(i)
	}
	group.Wait()
	statuses = []int{submits[0].status, submits[1].status}
	sort.Ints(statuses)
	if statuses[0] != 200 || statuses[1] != 201 || submits[0].err != nil || submits[1].err != nil {
		t.Fatalf("submission idempotency race: %+v", submits)
	}
	firstSubmission := number(t, object(t, submits[0].body)["id"])
	if firstSubmission != number(t, object(t, submits[1].body)["id"]) {
		t.Fatal("idempotent requests created different submissions")
	}
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM submissions WHERE selection_id=$1", selectionID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("submission count=%d, err=%v", count, err)
	}
	w := &worker.Worker{DB: db, Config: platform.Config{PluginTimeout: 10 * time.Second}, Runner: func(context.Context, worker.Job) (worker.Result, error) {
		return worker.Result{ProtocolVersion: "1", Score: 100}, nil
	}}
	if done, err := w.RunOnce(context.Background()); !done || err != nil {
		t.Fatalf("initial evaluation: done=%v err=%v", done, err)
	}
	selectionList := requireStatus(t, student.json(http.MethodGet, base+"/me/selections", nil, nil), 200).([]any)
	if score := object(t, selectionList[0])["score"]; score != float64(100) {
		t.Fatalf("student score=%v", score)
	}
	second := object(t, requireStatus(t, student.json(http.MethodPost, submitPath, payload, map[string]string{"Idempotency-Key": "integration-key-two"}), 201))
	secondJobID := number(t, second["evaluation_job_id"])
	started := make(chan struct{})
	release := make(chan struct{})
	first := &worker.Worker{DB: db, Config: platform.Config{PluginTimeout: 10 * time.Second}, Runner: func(ctx context.Context, _ worker.Job) (worker.Result, error) {
		close(started)
		select {
		case <-release:
			return worker.Result{ProtocolVersion: "1", Score: 10}, nil
		case <-ctx.Done():
			return worker.Result{}, ctx.Err()
		}
	}}
	firstDone := make(chan error, 1)
	go func() { _, err := first.RunOnce(context.Background()); firstDone <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first worker never claimed job")
	}
	secondWorker := &worker.Worker{DB: db, Config: platform.Config{PluginTimeout: 10 * time.Second}, Runner: func(context.Context, worker.Job) (worker.Result, error) {
		return worker.Result{ProtocolVersion: "1", Score: 92}, nil
	}}
	if done, err := secondWorker.RunOnce(context.Background()); done || err != nil {
		t.Fatalf("active lease was claimed twice: done=%v err=%v", done, err)
	}
	if _, err := db.Exec(context.Background(), "UPDATE evaluation_jobs SET lease_until=now()-interval '1 second' WHERE id=$1", secondJobID); err != nil {
		t.Fatal(err)
	}
	if done, err := secondWorker.RunOnce(context.Background()); !done || err != nil {
		t.Fatalf("expired lease not reclaimed: done=%v err=%v", done, err)
	}
	close(release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stale worker did not finish")
	}
	var score float64
	if err := db.QueryRow(context.Background(), "SELECT score FROM evaluation_results WHERE job_id=$1", secondJobID).Scan(&score); err != nil || score != 92 {
		t.Fatalf("stale worker overwrote score: score=%v err=%v", score, err)
	}
	third := object(t, requireStatus(t, student.json(http.MethodPost, submitPath, payload, map[string]string{"Idempotency-Key": "integration-key-three"}), 201))
	thirdJobID := number(t, third["evaluation_job_id"])
	failing := &worker.Worker{DB: db, Config: platform.Config{PluginTimeout: 10 * time.Second}, Runner: func(context.Context, worker.Job) (worker.Result, error) {
		return worker.Result{}, errors.New("plugin failed")
	}}
	if done, err := failing.RunOnce(context.Background()); !done || err != nil {
		t.Fatalf("failed evaluation cycle: done=%v err=%v", done, err)
	}
	selectionList = requireStatus(t, student.json(http.MethodGet, base+"/me/selections", nil, nil), 200).([]any)
	latest := object(t, selectionList[0])
	if latest["evaluation_status"] != "failed" || latest["score"] != nil {
		t.Fatalf("plugin failure must not become score zero: %v", latest)
	}
	retryPath := fmt.Sprintf("%s/admin/evaluations/%d/retry", base, thirdJobID)
	requireStatus(t, student.json(http.MethodPost, retryPath, nil, nil), 403)
	requireStatus(t, admin.json(http.MethodPost, retryPath, nil, nil), 200)
	if done, err := w.RunOnce(context.Background()); !done || err != nil {
		t.Fatalf("retry evaluation: done=%v err=%v", done, err)
	}
	requireStatus(t, admin.json(http.MethodPost, retryPath, nil, nil), 409)
	adminList := requireStatus(t, admin.json(http.MethodGet, base+"/admin/submissions", nil, nil), 200).([]any)
	if len(adminList) != 3 || object(t, adminList[0])["score"] != float64(100) {
		t.Fatalf("admin result list incorrect: %v", adminList)
	}
}
