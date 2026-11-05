package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
	"time"

	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/platform"
	"github.com/Wh1teJ0ker/birkenwald-recruit/pluginapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Job struct {
	ID           int64
	SubmissionID int64
	Attempt      int32
	LeaseToken   string
	LibraryPath  string
	PackagePath  string
	TaskID       int64
	Config       json.RawMessage
}

type Result = pluginapi.Result

type Runner func(context.Context, Job) (Result, error)

type Worker struct {
	DB     *pgxpool.Pool
	Config platform.Config
	Runner Runner

	// A timed-out Go plugin cannot be killed. Fail further jobs for that path
	// until the worker is restarted instead of calling into a potentially
	// still-running library concurrently.
	timedOut sync.Map
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Runner == nil {
		w.Runner = w.RunGoPlugin
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
		w.Runner = w.RunGoPlugin
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
	err = w.DB.QueryRow(ctx, `SELECT pv.library_path,f.object_key,sel.task_id,j.plugin_config_snapshot
        FROM evaluation_jobs j JOIN plugin_versions pv ON pv.id=j.plugin_version_id
        JOIN submissions sub ON sub.id=j.submission_id JOIN files f ON f.id=sub.package_file_id
        JOIN selections sel ON sel.id=sub.selection_id WHERE j.id=$1`, job.ID).
		Scan(&job.LibraryPath, &key, &job.TaskID, &job.Config)
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

func (w *Worker) RunGoPlugin(ctx context.Context, job Job) (Result, error) {
	var result Result
	if _, failed := w.timedOut.Load(job.LibraryPath); failed {
		return result, errors.New("plugin previously timed out; restart worker")
	}
	info, err := os.Stat(job.PackagePath)
	if err != nil || !info.Mode().IsRegular() {
		return result, errors.New("package file missing")
	}
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		var out outcome
		defer func() {
			if recovered := recover(); recovered != nil {
				out = outcome{err: fmt.Errorf("plugin panic: %v", recovered)}
			}
			done <- out
		}()
		loaded, err := plugin.Open(job.LibraryPath)
		if err != nil {
			out.err = fmt.Errorf("load plugin: %w", err)
			return
		}
		symbol, err := loaded.Lookup("Evaluate")
		if err != nil {
			out.err = fmt.Errorf("plugin Evaluate not found: %w", err)
			return
		}
		evaluate, ok := symbol.(func(context.Context, pluginapi.Request) (pluginapi.Result, error))
		if !ok {
			out.err = errors.New("plugin Evaluate has wrong signature")
			return
		}
		out.result, out.err = evaluate(ctx, pluginapi.Request{
			ProtocolVersion: pluginapi.ProtocolVersion,
			JobID:           job.ID,
			SubmissionID:    job.SubmissionID,
			TaskID:          job.TaskID,
			SubmissionPath:  job.PackagePath,
			Config:          job.Config,
		})
	}()
	select {
	case <-ctx.Done():
		w.timedOut.Store(job.LibraryPath, true)
		return result, fmt.Errorf("plugin timed out: %w", ctx.Err())
	case out := <-done:
		return out.result, out.err
	}
}
