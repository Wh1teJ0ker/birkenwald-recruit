package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Job struct {
	ID           int64
	SubmissionID int64
	Attempt      int32
	LeaseToken   string
	Image        string
	PackagePath  string
	TaskID       int64
	Config       json.RawMessage
}

type Result struct {
	ProtocolVersion string          `json:"protocol_version"`
	Score           float64         `json:"score"`
	Details         json.RawMessage `json:"details"`
}

type Runner func(context.Context, Job) (Result, error)

type Worker struct {
	DB     *pgxpool.Pool
	Config platform.Config
	Runner Runner
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Runner == nil {
		w.Runner = w.RunDocker
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		done, err := w.RunOnce(ctx)
		if err != nil {
			slog.Error("worker cycle failed", "error", err)
		}
		if done {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	if w.Runner == nil {
		w.Runner = w.RunDocker
	}
	job, err := w.claim(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	slog.Info("evaluation started", "job_id", job.ID, "attempt", job.Attempt)
	runCtx, cancel := context.WithTimeout(ctx, w.Config.PluginTimeout)
	result, runErr := w.Runner(runCtx, job)
	cancel()
	if runErr == nil {
		runErr = validateResult(result)
	}
	if err := w.finish(ctx, job, result, runErr); err != nil {
		return true, err
	}
	if runErr != nil {
		slog.Warn("evaluation failed", "job_id", job.ID, "error", runErr)
	} else {
		slog.Info("evaluation completed", "job_id", job.ID, "score", result.Score)
	}
	return true, nil
}

func (w *Worker) claim(ctx context.Context) (Job, error) {
	var job Job
	token, err := platform.RandomToken()
	if err != nil {
		return job, err
	}
	tx, err := w.DB.Begin(ctx)
	if err != nil {
		return job, err
	}
	defer tx.Rollback(ctx)
	leaseSeconds := int64(w.Config.PluginTimeout.Seconds()) + 30
	err = tx.QueryRow(ctx, `WITH candidate AS (
        SELECT id FROM evaluation_jobs
        WHERE status='queued' OR (status='running' AND lease_until<now())
        ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1
    ) UPDATE evaluation_jobs j SET status='running',attempt=attempt+1,
        lease_token=$1,lease_until=now()+($2 * interval '1 second'),error_text=NULL
        FROM candidate WHERE j.id=candidate.id
        RETURNING j.id,j.submission_id,j.attempt,j.lease_token`, token, leaseSeconds).
		Scan(&job.ID, &job.SubmissionID, &job.Attempt, &job.LeaseToken)
	if err != nil {
		return job, err
	}
	_, err = tx.Exec(ctx, "UPDATE submissions SET status='evaluating' WHERE id=$1", job.SubmissionID)
	if err != nil {
		return job, err
	}
	if err = tx.Commit(ctx); err != nil {
		return job, err
	}
	var key string
	err = w.DB.QueryRow(ctx, `SELECT pv.image_ref,f.object_key,sel.task_id,j.plugin_config_snapshot
        FROM evaluation_jobs j JOIN plugin_versions pv ON pv.id=j.plugin_version_id
        JOIN submissions sub ON sub.id=j.submission_id JOIN files f ON f.id=sub.package_file_id
        JOIN selections sel ON sel.id=sub.selection_id WHERE j.id=$1`, job.ID).
		Scan(&job.Image, &key, &job.TaskID, &job.Config)
	if err != nil {
		return job, err
	}
	if len(key) != 64 || strings.Trim(key, "0123456789abcdef") != "" {
		return job, errors.New("invalid package key")
	}
	job.PackagePath = filepath.Join(w.Config.UploadDir, key)
	return job, nil
}

func validateResult(r Result) error {
	if r.ProtocolVersion != "1" {
		return errors.New("unsupported plugin protocol version")
	}
	if math.IsNaN(r.Score) || math.IsInf(r.Score, 0) || r.Score < 0 || r.Score > 100 {
		return errors.New("plugin score out of range")
	}
	if len(r.Details) > 32768 {
		return errors.New("plugin details too large")
	}
	if len(r.Details) > 0 && !json.Valid(r.Details) {
		return errors.New("invalid plugin details")
	}
	return nil
}

func (w *Worker) finish(ctx context.Context, job Job, result Result, runErr error) error {
	tx, err := w.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var submissionID int64
	var status string
	var leaseToken *string
	var attempt int32
	err = tx.QueryRow(ctx, "SELECT submission_id,status,lease_token,attempt FROM evaluation_jobs WHERE id=$1 FOR UPDATE", job.ID).
		Scan(&submissionID, &status, &leaseToken, &attempt)
	if err != nil {
		return err
	}
	// A stale process must not finish a job already claimed by a newer worker.
	if status != "running" || leaseToken == nil || *leaseToken != job.LeaseToken || attempt != job.Attempt {
		return nil
	}
	if runErr == nil {
		details := result.Details
		if len(details) == 0 {
			details = []byte("{}")
		}
		_, err = tx.Exec(ctx, "INSERT INTO evaluation_results(job_id,score,details) VALUES($1,$2,$3) ON CONFLICT(job_id) DO NOTHING", job.ID, result.Score, details)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE evaluation_jobs SET status='succeeded',lease_token=NULL,lease_until=NULL,finished_at=now() WHERE id=$1", job.ID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE submissions SET status='evaluated' WHERE id=$1", submissionID)
	} else {
		message := runErr.Error()
		if len(message) > 500 {
			message = message[:500]
		}
		_, err = tx.Exec(ctx, "UPDATE evaluation_jobs SET status='failed',error_text=$2,lease_token=NULL,lease_until=NULL,finished_at=now() WHERE id=$1", job.ID, message)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE submissions SET status='failed' WHERE id=$1", submissionID)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	room := b.limit - b.Len()
	if room > 0 {
		if room > len(p) {
			room = len(p)
		}
		_, _ = b.Buffer.Write(p[:room])
	}
	return n, nil
}

func (w *Worker) RunDocker(ctx context.Context, job Job) (Result, error) {
	var result Result
	info, err := os.Stat(job.PackagePath)
	if err != nil || !info.Mode().IsRegular() {
		return result, errors.New("package file missing")
	}
	input := map[string]any{"protocol_version": "1", "job_id": job.ID, "submission_id": job.SubmissionID, "task_id": job.TaskID, "config": json.RawMessage(job.Config)}
	encoded, err := json.Marshal(input)
	if err != nil {
		return result, err
	}
	args := []string{"run", "--rm", "-i", "--pull=never", "--network=none", "--read-only", "--pids-limit=64", "--memory=256m", "--cpus=1",
		fmt.Sprintf("--user=%d:%d", os.Getuid(), os.Getgid()), "--security-opt=no-new-privileges", "--cap-drop=ALL",
		"--mount", "type=bind,src=" + job.PackagePath + ",dst=/input/submission.zip,readonly", job.Image}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = bytes.NewReader(encoded)
	stdout := &limitedBuffer{limit: 65536}
	stderr := &limitedBuffer{limit: 16384}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("plugin timed out: %w", ctx.Err())
		}
		return result, fmt.Errorf("plugin exited: %v; %s", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() >= stdout.limit {
		return result, errors.New("plugin output too large")
	}
	text := strings.TrimSpace(stdout.String())
	if strings.Contains(text, "\n") {
		return result, errors.New("plugin must output one JSON line")
	}
	if err = json.Unmarshal([]byte(text), &result); err != nil {
		return result, fmt.Errorf("invalid plugin JSON: %w", err)
	}
	return result, nil
}
