package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/platform"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type selectInput struct {
	TaskID int64 `json:"task_id"`
}

func (s *Server) selectTask(c *gin.Context) {
	campaignID, ok := parseID(c, "id")
	if !ok {
		return
	}
	if currentUser(c).Role != "student" {
		fail(c, 403, "student required")
		return
	}
	var in selectInput
	if err := c.ShouldBindJSON(&in); err != nil || in.TaskID <= 0 {
		fail(c, 422, "task_id required")
		return
	}
	tx, err := s.DB.Begin(c.Request.Context())
	if err != nil {
		fail(c, 500, "selection failed")
		return
	}
	defer tx.Rollback(c.Request.Context())
	var existingID, existingTask int64
	err = tx.QueryRow(c.Request.Context(), "SELECT id,task_id FROM selections WHERE campaign_id=$1 AND student_id=$2", campaignID, currentUser(c).ID).Scan(&existingID, &existingTask)
	if err == nil {
		c.JSON(200, gin.H{"id": existingID, "task_id": existingTask, "already_selected": true})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		fail(c, 500, "selection failed")
		return
	}
	var campaignStatus, taskStatus string
	var opens, closes, deadline, now time.Time
	var capacity *int32
	var selected int32
	err = tx.QueryRow(c.Request.Context(), `SELECT c.status,c.opens_at,c.closes_at,t.status,t.capacity,t.selected_count,t.deadline_at,now()
        FROM tasks t JOIN campaigns c ON c.id=t.campaign_id
        WHERE c.id=$1 AND t.id=$2 FOR SHARE OF c FOR UPDATE OF t`, campaignID, in.TaskID).
		Scan(&campaignStatus, &opens, &closes, &taskStatus, &capacity, &selected, &deadline, &now)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "task not found")
		return
	}
	if err != nil {
		fail(c, 500, "selection failed")
		return
	}
	if campaignStatus != "open" || taskStatus != "published" || now.Before(opens) || !now.Before(closes) || !now.Before(deadline) {
		fail(c, 409, "task is not open")
		return
	}
	if capacity != nil && selected >= *capacity {
		fail(c, 409, "task is full")
		return
	}
	var id int64
	err = tx.QueryRow(c.Request.Context(), `INSERT INTO selections(campaign_id,student_id,task_id)
        VALUES($1,$2,$3) ON CONFLICT(campaign_id,student_id) DO NOTHING RETURNING id`, campaignID, currentUser(c).ID, in.TaskID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 409, "another task was already selected")
		return
	}
	if err != nil {
		fail(c, 500, "selection failed")
		return
	}
	if _, err = tx.Exec(c.Request.Context(), "UPDATE tasks SET selected_count=selected_count+1 WHERE id=$1", in.TaskID); err != nil {
		fail(c, 500, "selection failed")
		return
	}
	if _, err = tx.Exec(c.Request.Context(), "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,'task.select','selection',$2)", currentUser(c).ID, id); err != nil {
		fail(c, 500, "selection failed")
		return
	}
	if err = tx.Commit(c.Request.Context()); err != nil {
		fail(c, 500, "selection failed")
		return
	}
	c.JSON(201, gin.H{"id": id, "task_id": in.TaskID})
}

func (s *Server) mySelections(c *gin.Context) {
	rows, err := s.DB.Query(c.Request.Context(), `SELECT sel.id,sel.campaign_id,sel.task_id,t.title,
        sub.id,sub.version,sub.status,sub.submitted_at,j.id,j.status,r.score,j.error_text
        FROM selections sel JOIN tasks t ON t.id=sel.task_id
        LEFT JOIN submissions sub ON sub.id=sel.current_submission_id
        LEFT JOIN evaluation_jobs j ON j.submission_id=sub.id
        LEFT JOIN evaluation_results r ON r.job_id=j.id
        WHERE sel.student_id=$1 ORDER BY sel.created_at DESC`, currentUser(c).ID)
	if err != nil {
		fail(c, 500, "list selections failed")
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id, campaignID, taskID int64
		var title string
		var submissionID, jobID *int64
		var version *int32
		var subStatus, jobStatus, errorText *string
		var submittedAt *time.Time
		var score *float64
		if err := rows.Scan(&id, &campaignID, &taskID, &title, &submissionID, &version, &subStatus, &submittedAt, &jobID, &jobStatus, &score, &errorText); err != nil {
			fail(c, 500, "list selections failed")
			return
		}
		items = append(items, gin.H{"id": id, "campaign_id": campaignID, "task_id": taskID, "task_title": title,
			"submission_id": submissionID, "version": version, "submission_status": subStatus, "submitted_at": submittedAt,
			"evaluation_job_id": jobID, "evaluation_status": jobStatus, "score": score, "error": errorText})
	}
	if rows.Err() != nil {
		fail(c, 500, "list selections failed")
		return
	}
	c.JSON(200, items)
}

func (s *Server) uploadFile(c *gin.Context) {
	if currentUser(c).Role != "student" {
		fail(c, 403, "student required")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 51<<20)
	kind := c.PostForm("kind")
	header, err := c.FormFile("file")
	if err != nil {
		fail(c, 422, "file required or too large")
		return
	}
	saved, err := platform.SaveUpload(s.Config.UploadDir, kind, header)
	if err != nil {
		fail(c, 422, err.Error())
		return
	}
	var id int64
	err = s.DB.QueryRow(c.Request.Context(), `INSERT INTO files(owner_id,kind,object_key,original_name,sha256,size_bytes)
        VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, currentUser(c).ID, kind, saved.Key, saved.Name, saved.SHA256, saved.Size).Scan(&id)
	if err != nil {
		os.Remove(saved.Path)
		fail(c, 500, "save file failed")
		return
	}
	c.JSON(201, gin.H{"id": id, "kind": kind, "name": saved.Name, "size_bytes": saved.Size, "sha256": saved.SHA256})
}

func (s *Server) downloadFile(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var owner int64
	var key, name string
	err := s.DB.QueryRow(c.Request.Context(), "SELECT owner_id,object_key,original_name FROM files WHERE id=$1", id).Scan(&owner, &key, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "file not found")
		return
	}
	if err != nil {
		fail(c, 500, "download failed")
		return
	}
	if owner != currentUser(c).ID && currentUser(c).Role != "admin" {
		fail(c, 403, "access denied")
		return
	}
	if len(key) != 64 || strings.Trim(key, "0123456789abcdef") != "" {
		fail(c, 500, "invalid stored file key")
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store")
	c.FileAttachment(filepath.Join(s.Config.UploadDir, key), name)
}

type submissionInput struct {
	ResumeFileID  int64 `json:"resume_file_id"`
	PackageFileID int64 `json:"package_file_id"`
}

func (s *Server) submit(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if currentUser(c).Role != "student" {
		fail(c, 403, "student required")
		return
	}
	requestKey := c.GetHeader("Idempotency-Key")
	if len(requestKey) < 8 || len(requestKey) > 128 {
		fail(c, 422, "Idempotency-Key of 8-128 characters required")
		return
	}
	var in submissionInput
	if err := c.ShouldBindJSON(&in); err != nil || in.ResumeFileID <= 0 || in.PackageFileID <= 0 || in.ResumeFileID == in.PackageFileID {
		fail(c, 422, "resume_file_id and package_file_id required")
		return
	}
	tx, err := s.DB.Begin(c.Request.Context())
	if err != nil {
		fail(c, 500, "submission failed")
		return
	}
	defer tx.Rollback(c.Request.Context())
	var taskID, pluginID int64
	var deadline, closes, now time.Time
	var config json.RawMessage
	err = tx.QueryRow(c.Request.Context(), `SELECT sel.task_id,t.plugin_version_id,t.plugin_config,t.deadline_at,c.closes_at,now()
        FROM selections sel JOIN tasks t ON t.id=sel.task_id JOIN campaigns c ON c.id=sel.campaign_id
        WHERE sel.id=$1 AND sel.student_id=$2 FOR UPDATE OF sel FOR SHARE OF t,c`, id, currentUser(c).ID).
		Scan(&taskID, &pluginID, &config, &deadline, &closes, &now)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "selection not found")
		return
	}
	if err != nil {
		fail(c, 500, "submission failed")
		return
	}
	var previousID int64
	var previousVersion int32
	err = tx.QueryRow(c.Request.Context(), "SELECT id,version FROM submissions WHERE selection_id=$1 AND idempotency_key=$2", id, requestKey).Scan(&previousID, &previousVersion)
	if err == nil {
		c.JSON(200, gin.H{"id": previousID, "version": previousVersion, "already_submitted": true})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		fail(c, 500, "submission failed")
		return
	}
	if !now.Before(deadline) || !now.Before(closes) {
		fail(c, 409, "submission deadline passed")
		return
	}
	for _, f := range []struct {
		id   int64
		kind string
	}{{in.ResumeFileID, "resume"}, {in.PackageFileID, "package"}} {
		var kind string
		err = tx.QueryRow(c.Request.Context(), "SELECT kind FROM files WHERE id=$1 AND owner_id=$2", f.id, currentUser(c).ID).Scan(&kind)
		if err != nil || kind != f.kind {
			fail(c, 422, "file does not belong to student or has wrong kind")
			return
		}
	}
	var version int32
	err = tx.QueryRow(c.Request.Context(), "SELECT COALESCE(MAX(version),0)+1 FROM submissions WHERE selection_id=$1", id).Scan(&version)
	if err != nil {
		fail(c, 500, "submission failed")
		return
	}
	var submissionID, jobID int64
	err = tx.QueryRow(c.Request.Context(), `INSERT INTO submissions(selection_id,version,resume_file_id,package_file_id,idempotency_key)
        VALUES($1,$2,$3,$4,$5) RETURNING id`, id, version, in.ResumeFileID, in.PackageFileID, requestKey).Scan(&submissionID)
	if err != nil {
		fail(c, 500, "submission failed")
		return
	}
	_, err = tx.Exec(c.Request.Context(), "UPDATE selections SET current_submission_id=$1 WHERE id=$2", submissionID, id)
	if err != nil {
		fail(c, 500, "submission failed")
		return
	}
	err = tx.QueryRow(c.Request.Context(), `INSERT INTO evaluation_jobs(submission_id,plugin_version_id,plugin_config_snapshot)
        VALUES($1,$2,$3) RETURNING id`, submissionID, pluginID, config).Scan(&jobID)
	if err != nil {
		fail(c, 500, "submission failed")
		return
	}
	_, err = tx.Exec(c.Request.Context(), "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,'submission.create','submission',$2)", currentUser(c).ID, submissionID)
	if err != nil {
		fail(c, 500, "submission failed")
		return
	}
	if err = tx.Commit(c.Request.Context()); err != nil {
		fail(c, 500, "submission failed")
		return
	}
	c.JSON(201, gin.H{"id": submissionID, "version": version, "evaluation_job_id": jobID})
}

func (s *Server) adminSubmissions(c *gin.Context) {
	rows, err := s.DB.Query(c.Request.Context(), `SELECT sub.id,sub.version,sub.status,sub.submitted_at,u.email,u.display_name,
        t.title,sub.resume_file_id,sub.package_file_id,j.id,j.status,r.score,j.error_text
        FROM submissions sub JOIN selections sel ON sel.id=sub.selection_id
        JOIN users u ON u.id=sel.student_id JOIN tasks t ON t.id=sel.task_id
        JOIN evaluation_jobs j ON j.submission_id=sub.id
        LEFT JOIN evaluation_results r ON r.job_id=j.id ORDER BY sub.id DESC LIMIT 500`)
	if err != nil {
		fail(c, 500, "list submissions failed")
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id, version, jobID, resumeID, packageID int64
		var status, email, name, task, jobStatus string
		var submittedAt time.Time
		var score *float64
		var errorText *string
		if err := rows.Scan(&id, &version, &status, &submittedAt, &email, &name, &task, &resumeID, &packageID, &jobID, &jobStatus, &score, &errorText); err != nil {
			fail(c, 500, "list submissions failed")
			return
		}
		items = append(items, gin.H{"id": id, "version": version, "status": status, "submitted_at": submittedAt, "student_email": email, "student_name": name,
			"task_title": task, "resume_file_id": resumeID, "package_file_id": packageID, "evaluation_job_id": jobID, "evaluation_status": jobStatus, "score": score, "error": errorText})
	}
	if rows.Err() != nil {
		fail(c, 500, "list submissions failed")
		return
	}
	c.JSON(200, items)
}

func (s *Server) retryEvaluation(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	tx, err := s.DB.Begin(c.Request.Context())
	if err != nil {
		fail(c, 500, "retry failed")
		return
	}
	defer tx.Rollback(c.Request.Context())
	var submissionID int64
	err = tx.QueryRow(c.Request.Context(), `UPDATE evaluation_jobs SET status='queued',error_text=NULL,lease_token=NULL,lease_until=NULL,finished_at=NULL
        WHERE id=$1 AND status='failed' RETURNING submission_id`, id).Scan(&submissionID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 409, "only failed jobs can be retried")
		return
	}
	if err != nil {
		fail(c, 500, "retry failed")
		return
	}
	if _, err = tx.Exec(c.Request.Context(), "UPDATE submissions SET status='submitted' WHERE id=$1", submissionID); err != nil {
		fail(c, 500, "retry failed")
		return
	}
	if _, err = tx.Exec(c.Request.Context(), "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,'evaluation.retry','evaluation_job',$2)", currentUser(c).ID, id); err != nil {
		fail(c, 500, "retry failed")
		return
	}
	if err = tx.Commit(c.Request.Context()); err != nil {
		fail(c, 500, "retry failed")
		return
	}
	c.JSON(200, gin.H{"id": id, "status": "queued"})
}
