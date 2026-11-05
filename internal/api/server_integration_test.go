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
	"strings"
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

func (c *testClient) uploadPlugin(fields map[string]string, name string, data []byte) apiResponse {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := w.WriteField(key, value); err != nil {
			return apiResponse{err: err}
		}
	}
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
	return c.do(http.MethodPost, "/api/v1/admin/plugins/upload", w.FormDataContentType(), body.Bytes(), nil)
}

func (c *testClient) rawGet(path string) (int, http.Header, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Origin", c.base)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, data, err
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
	return &testClient{base: server.URL, http: &http.Client{Jar: jar, Timeout: 2 * time.Minute}}
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
	s := httptest.NewServer((&api.Server{DB: db, Config: platform.Config{UploadDir: t.TempDir(), PluginTimeout: 10 * time.Second, EmailDevMode: true}}).Router())
	t.Cleanup(s.Close)
	return db, s, adminEmail
}

func registerStudent(t *testing.T, client *testClient, email, name string) map[string]any {
	t.Helper()
	sent := object(t, requireStatus(t, client.json(http.MethodPost, "/api/v1/auth/verification-code", map[string]any{"email": email}, nil), 202))
	code, _ := sent["dev_code"].(string)
	if code == "" {
		t.Fatal("missing dev verification code")
	}
	return object(t, requireStatus(t, client.json(http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email": email, "name": name, "password": "test-student-password-123", "code": code,
	}, nil), 201))
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
		registerStudent(t, student, fmt.Sprintf("student%d-%d@example.org", i, time.Now().UnixNano()), "Student")
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
	thirdRegistration := registerStudent(t, thirdStudent, fmt.Sprintf("student3-%d@example.org", time.Now().UnixNano()), "Third Student")
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

func TestEmailVerificationRegistration(t *testing.T) {
	_, server, _ := testServer(t)
	client := clientFor(t, server)
	base := "/api/v1"
	email := fmt.Sprintf("verify-%d@example.org", time.Now().UnixNano())

	requireStatus(t, client.json(http.MethodPost, base+"/auth/register", map[string]any{
		"email": email, "name": "Verify", "password": "test-student-password-123", "code": "123456",
	}, nil), 422)

	sent := object(t, requireStatus(t, client.json(http.MethodPost, base+"/auth/verification-code", map[string]any{"email": email}, nil), 202))
	code, _ := sent["dev_code"].(string)
	if len(code) != 6 {
		t.Fatalf("unexpected dev verification code: %v", sent)
	}
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	requireStatus(t, client.json(http.MethodPost, base+"/auth/register", map[string]any{
		"email": email, "name": "Verify", "password": "test-student-password-123", "code": wrong,
	}, nil), 422)
	requireStatus(t, client.json(http.MethodPost, base+"/auth/register", map[string]any{
		"email": email, "name": "Verify", "password": "test-student-password-123", "code": code,
	}, nil), 201)
	me := object(t, requireStatus(t, client.json(http.MethodGet, base+"/me", nil, nil), 200))
	if me["role"] != "student" {
		t.Fatalf("registration did not create a student session: %v", me)
	}
	requireStatus(t, client.json(http.MethodPost, base+"/auth/verification-code", map[string]any{"email": email}, nil), 409)

	other := fmt.Sprintf("cooldown-%d@example.org", time.Now().UnixNano())
	requireStatus(t, client.json(http.MethodPost, base+"/auth/verification-code", map[string]any{"email": other}, nil), 202)
	requireStatus(t, client.json(http.MethodPost, base+"/auth/verification-code", map[string]any{"email": other}, nil), 429)
}

func TestAdminManagement(t *testing.T) {
	db, server, adminEmail := testServer(t)
	base := "/api/v1"
	admin := clientFor(t, server)
	requireStatus(t, admin.json(http.MethodPost, base+"/auth/login", map[string]any{"email": adminEmail, "password": "test-admin-password-123"}, nil), 200)
	admin.loadCSRF(t)
	adminID := number(t, object(t, requireStatus(t, admin.json(http.MethodGet, base+"/me", nil, nil), 200))["id"])

	now := time.Now().UTC()
	campaign := object(t, requireStatus(t, admin.json(http.MethodPost, base+"/admin/campaigns", map[string]any{
		"name": "Admin management", "opens_at": now.Add(-time.Minute), "closes_at": now.Add(3 * time.Hour), "open": true,
	}, nil), 201))
	campaignID := number(t, campaign["id"])
	task := object(t, requireStatus(t, admin.json(http.MethodPost, base+"/admin/tasks", map[string]any{
		"campaign_id": campaignID, "title": "Managed task", "description": "before", "deadline_at": now.Add(2 * time.Hour),
		"capacity": 2, "plugin_version_id": 1, "plugin_config": map[string]any{}, "publish": true,
	}, nil), 201))
	taskID := number(t, task["id"])

	studentA := clientFor(t, server)
	emailA := fmt.Sprintf("admin-manage-a-%d@example.org", time.Now().UnixNano())
	registerStudent(t, studentA, emailA, "Student A")
	studentA.loadCSRF(t)
	requireStatus(t, studentA.json(http.MethodPost, fmt.Sprintf("%s/campaigns/%d/selections", base, campaignID), map[string]any{"task_id": taskID}, nil), 201)

	studentB := clientFor(t, server)
	emailB := fmt.Sprintf("admin-manage-b-%d@example.org", time.Now().UnixNano())
	registerStudent(t, studentB, emailB, "Student B")
	studentB.loadCSRF(t)
	selectionB := object(t, requireStatus(t, studentB.json(http.MethodPost, fmt.Sprintf("%s/campaigns/%d/selections", base, campaignID), map[string]any{"task_id": taskID}, nil), 201))
	selectionBID := number(t, selectionB["id"])
	resume := object(t, requireStatus(t, studentB.upload("resume", "resume.pdf", []byte("%PDF-1.4\n%%EOF")), 201))
	pack := object(t, requireStatus(t, studentB.upload("package", "work.zip", createZip(t)), 201))

	studentC := clientFor(t, server)
	emailC := fmt.Sprintf("admin-manage-c-%d@example.org", time.Now().UnixNano())
	registerStudent(t, studentC, emailC, "Student C")
	studentC.loadCSRF(t)

	requireStatus(t, studentC.json(http.MethodGet, base+"/admin/users", nil, nil), 403)
	requireStatus(t, studentC.json(http.MethodGet, base+"/admin/settings", nil, nil), 403)

	usersBody, ok := requireStatus(t, admin.json(http.MethodGet, base+"/admin/users?q="+emailA, nil, nil), 200).([]any)
	if !ok || len(usersBody) != 1 {
		t.Fatalf("user search returned %T: %v", usersBody, usersBody)
	}
	userA := object(t, usersBody[0])
	studentAID := number(t, userA["id"])
	if userA["email"] != emailA || number(t, userA["selection_count"]) != 1 || userA["disabled"] != false {
		t.Fatalf("unexpected user list row: %v", userA)
	}

	requireStatus(t, admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/users/%d", base, adminID), map[string]any{"disabled": true}, nil), 409)
	requireStatus(t, admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/users/%d", base, studentAID), map[string]any{"role": "admin"}, nil), 200)
	adminStudentA := clientFor(t, server)
	requireStatus(t, adminStudentA.json(http.MethodPost, base+"/auth/login", map[string]any{"email": emailA, "password": "test-student-password-123"}, nil), 200)
	adminStudentA.loadCSRF(t)
	requireStatus(t, adminStudentA.json(http.MethodGet, base+"/admin/users", nil, nil), 200)
	requireStatus(t, admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/users/%d", base, studentAID), map[string]any{"role": "student"}, nil), 200)
	requireStatus(t, admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/users/%d", base, studentAID), map[string]any{"disabled": true}, nil), 200)
	requireStatus(t, studentA.json(http.MethodGet, base+"/me", nil, nil), 401)
	requireStatus(t, clientFor(t, server).json(http.MethodPost, base+"/auth/login", map[string]any{"email": emailA, "password": "test-student-password-123"}, nil), 403)
	disabledBody, ok := requireStatus(t, admin.json(http.MethodGet, base+"/admin/users?status=disabled", nil, nil), 200).([]any)
	if !ok || len(disabledBody) != 1 || object(t, disabledBody[0])["email"] != emailA {
		t.Fatalf("disabled filter returned: %v", disabledBody)
	}
	requireStatus(t, admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/users/%d", base, studentAID), map[string]any{"disabled": false}, nil), 200)
	reloginA := clientFor(t, server)
	requireStatus(t, reloginA.json(http.MethodPost, base+"/auth/login", map[string]any{"email": emailA, "password": "test-student-password-123"}, nil), 200)

	taskUpdate := func(changes map[string]any) apiResponse {
		body := map[string]any{
			"title": "Managed task", "description": "updated", "deadline_at": now.Add(2 * time.Hour), "capacity": 2, "status": "closed",
		}
		for key, value := range changes {
			body[key] = value
		}
		return admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/tasks/%d", base, taskID), body, nil)
	}
	requireStatus(t, taskUpdate(map[string]any{"deadline_at": now.Add(4 * time.Hour)}), 422)
	requireStatus(t, taskUpdate(map[string]any{"capacity": 1}), 409)
	requireStatus(t, taskUpdate(nil), 200)
	requireStatus(t, studentC.json(http.MethodPost, fmt.Sprintf("%s/campaigns/%d/selections", base, campaignID), map[string]any{"task_id": taskID}, nil), 409)
	requireStatus(t, studentB.json(http.MethodPost, fmt.Sprintf("%s/selections/%d/submissions", base, selectionBID), map[string]any{
		"resume_file_id": number(t, resume["id"]), "package_file_id": number(t, pack["id"]),
	}, map[string]string{"Idempotency-Key": "closed-submission-0001"}), 409)
	if _, err := db.Exec(context.Background(), "UPDATE tasks SET status='published' WHERE id=$1", taskID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/campaigns/%d", base, campaignID), map[string]any{"status": "closed"}, nil), 200)
	visibleCampaigns, ok := requireStatus(t, studentC.json(http.MethodGet, base+"/campaigns", nil, nil), 200).([]any)
	if !ok {
		t.Fatalf("campaign list returned %T", visibleCampaigns)
	}
	for _, item := range visibleCampaigns {
		if number(t, object(t, item)["id"]) == campaignID {
			t.Fatalf("closed campaign still visible: %v", item)
		}
	}

	settings := object(t, requireStatus(t, admin.json(http.MethodGet, base+"/admin/settings", nil, nil), 200))
	if settings["version"] == "" || settings["counts"] == nil || settings["database_version"] == "" {
		t.Fatalf("settings missing system info: %v", settings)
	}
	if _, leaked := settings["database_url"]; leaked {
		t.Fatalf("settings leaked database url: %v", settings)
	}
	requireStatus(t, admin.json(http.MethodPatch, base+"/admin/settings", map[string]any{
		"site_name": "白桦林", "season_year": "2027", "registration_enabled": false,
	}, nil), 200)
	config := object(t, requireStatus(t, clientFor(t, server).json(http.MethodGet, base+"/config", nil, nil), 200))
	if config["site_name"] != "白桦林" || config["season_year"] != "2027" || config["registration_enabled"] != false {
		t.Fatalf("settings did not update public config: %v", config)
	}
	requireStatus(t, admin.json(http.MethodPatch, base+"/admin/settings", map[string]any{
		"smtp_host": "smtp.example.org", "smtp_port": "2525", "smtp_username": "mailer",
		"smtp_password": "smtp-secret", "smtp_from": "no-reply@example.org",
	}, nil), 200)
	mailSettings := object(t, requireStatus(t, admin.json(http.MethodGet, base+"/admin/settings", nil, nil), 200))
	if mailSettings["smtp_host"] != "smtp.example.org" || number(t, mailSettings["smtp_port"]) != 2525 ||
		mailSettings["smtp_password_set"] != true || mailSettings["smtp_password_saved"] != true {
		t.Fatalf("mail settings not saved: %v", mailSettings)
	}
	if _, leaked := mailSettings["smtp_password"]; leaked {
		t.Fatalf("mail settings leaked smtp password: %v", mailSettings)
	}
	newEmail := fmt.Sprintf("closed-registration-%d@example.org", time.Now().UnixNano())
	requireStatus(t, clientFor(t, server).json(http.MethodPost, base+"/auth/verification-code", map[string]any{"email": newEmail}, nil), 403)
	requireStatus(t, clientFor(t, server).json(http.MethodPost, base+"/auth/register", map[string]any{
		"email": newEmail, "name": "Closed", "password": "test-student-password-123", "code": "123456",
	}, nil), 403)
	requireStatus(t, admin.json(http.MethodPatch, base+"/admin/settings", map[string]any{"registration_enabled": true}, nil), 200)
	requireStatus(t, clientFor(t, server).json(http.MethodPost, base+"/auth/verification-code", map[string]any{"email": newEmail}, nil), 202)
}

func TestPluginManagement(t *testing.T) {
	_, server, adminEmail := testServer(t)
	base := "/api/v1"
	admin := clientFor(t, server)
	requireStatus(t, admin.json(http.MethodPost, base+"/auth/login", map[string]any{"email": adminEmail, "password": "test-admin-password-123"}, nil), 200)
	admin.loadCSRF(t)

	initial, ok := requireStatus(t, admin.json(http.MethodGet, base+"/admin/plugins", nil, nil), 200).([]any)
	if !ok || len(initial) != 1 || object(t, initial[0])["enabled"] != true {
		t.Fatalf("unexpected initial plugins: %v", initial)
	}
	created := object(t, requireStatus(t, admin.json(http.MethodPost, base+"/admin/plugins", map[string]any{
		"name": "custom-check", "version": "1.0.0", "library_path": "/tmp/custom-check.so",
		"config_schema": map[string]any{"type": "object"}, "enabled": true,
	}, nil), 201))
	pluginID := number(t, created["id"])
	requireStatus(t, admin.json(http.MethodPost, base+"/admin/plugins", map[string]any{
		"name": "custom-check", "version": "1.0.0", "library_path": "/tmp/custom-check.so", "config_schema": map[string]any{},
	}, nil), 409)
	requireStatus(t, admin.json(http.MethodPost, base+"/admin/plugins", map[string]any{
		"name": "bad-check", "version": "1.0.0", "library_path": "/tmp/bad-check.so", "config_schema": []string{"object"},
	}, nil), 422)
	requireStatus(t, admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/plugins/%d", base, pluginID), map[string]any{
		"library_path": "/tmp/not-a-library",
	}, nil), 422)
	updated := object(t, requireStatus(t, admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/plugins/%d", base, pluginID), map[string]any{
		"library_path": "/tmp/custom-check-v2.so",
	}, nil), 200))
	if updated["library_path"] != "/tmp/custom-check-v2.so" {
		t.Fatalf("plugin library path not updated: %v", updated)
	}
	all, ok := requireStatus(t, admin.json(http.MethodGet, base+"/admin/plugins?all=true", nil, nil), 200).([]any)
	if !ok || len(all) != 2 {
		t.Fatalf("all plugins: %T %v", all, all)
	}

	requireStatus(t, admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/plugins/%d", base, pluginID), map[string]any{"enabled": false}, nil), 200)
	enabledOnly, ok := requireStatus(t, admin.json(http.MethodGet, base+"/admin/plugins", nil, nil), 200).([]any)
	if !ok || len(enabledOnly) != 1 {
		t.Fatalf("disabled plugin still listed as enabled: %v", enabledOnly)
	}

	now := time.Now().UTC()
	campaign := object(t, requireStatus(t, admin.json(http.MethodPost, base+"/admin/campaigns", map[string]any{
		"name": "Plugin management", "opens_at": now.Add(-time.Minute), "closes_at": now.Add(2 * time.Hour), "open": true,
	}, nil), 201))
	taskBody := map[string]any{
		"campaign_id": number(t, campaign["id"]), "title": "Plugin task", "description": "test",
		"deadline_at": now.Add(time.Hour), "capacity": 5, "plugin_version_id": pluginID,
		"plugin_config": map[string]any{}, "publish": true,
	}
	requireStatus(t, admin.json(http.MethodPost, base+"/admin/tasks", taskBody, nil), 422)
	requireStatus(t, admin.json(http.MethodPatch, fmt.Sprintf("%s/admin/plugins/%d", base, pluginID), map[string]any{"enabled": true}, nil), 200)
	requireStatus(t, admin.json(http.MethodPost, base+"/admin/tasks", taskBody, nil), 201)
}

func TestPluginSourceCompilation(t *testing.T) {
	_, server, adminEmail := testServer(t)
	base := "/api/v1"
	admin := clientFor(t, server)
	requireStatus(t, admin.json(http.MethodPost, base+"/auth/login", map[string]any{"email": adminEmail, "password": "test-admin-password-123"}, nil), 200)
	admin.loadCSRF(t)

	source, err := os.ReadFile("../../plugins/example/main.go")
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{
		"name": "uploaded-example", "version": "1.0.0",
		"config_schema": `{"type":"object"}`, "enabled": "true",
	}
	created := object(t, requireStatus(t, admin.uploadPlugin(fields, "plugin.go", source), 201))
	libraryPath, _ := created["library_path"].(string)
	if !strings.HasSuffix(libraryPath, ".so") {
		t.Fatalf("uploaded plugin path is not a .so: %q", libraryPath)
	}
	info, err := os.Stat(libraryPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("compiled plugin is missing: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(libraryPath) })

	requireStatus(t, admin.uploadPlugin(fields, "plugin.go", source), 409)
	requireStatus(t, admin.uploadPlugin(map[string]string{
		"name": "cgo-plugin", "version": "1.0.0", "config_schema": `{}`,
	}, "plugin.go", []byte("package main\n\n/* #cgo LDFLAGS: -lm */\nimport \"C\"\n\nfunc main() {}\n")), 422)
	requireStatus(t, admin.uploadPlugin(map[string]string{
		"name": "bad-plugin", "version": "1.0.0", "config_schema": `{}`,
	}, "README.md", []byte("# not a plugin")), 422)

	status, headers, body, err := admin.rawGet(base + "/admin/plugins/example/package")
	if err != nil || status != 200 {
		t.Fatalf("example package download failed: status=%d err=%v", status, err)
	}
	if !strings.Contains(headers.Get("Content-Disposition"), "example-plugin.zip") {
		t.Fatalf("unexpected package disposition: %q", headers.Get("Content-Disposition"))
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("example package is not a valid zip: %v", err)
	}
	entries := make(map[string]string)
	for _, entry := range archive.File {
		stream, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[entry.Name] = string(content)
	}
	if !strings.Contains(entries["example-plugin/main.go"], "func Evaluate") {
		t.Fatalf("example package is missing main.go: %v", entries)
	}
	if !strings.Contains(entries["example-plugin/AGENTS.md"], "Required package and symbol") {
		t.Fatalf("example package is missing agent docs: %v", entries)
	}
	if !strings.Contains(entries["example-plugin/README.md"], "Build") {
		t.Fatalf("example package is missing README: %v", entries)
	}
	packaged := object(t, requireStatus(t, admin.uploadPlugin(map[string]string{
		"name": "uploaded-example-zip", "version": "1.0.0", "config_schema": `{}`,
	}, "example-plugin.zip", body), 201))
	packagedPath, _ := packaged["library_path"].(string)
	t.Cleanup(func() { _ = os.Remove(packagedPath) })

	status, _, body, err = admin.rawGet(base + "/admin/plugins/example/binary")
	if err != nil || status != 200 || len(body) == 0 {
		t.Fatalf("example binary download failed: status=%d err=%v", status, err)
	}
}
