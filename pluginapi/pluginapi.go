// Package pluginapi defines the contract between the worker and Go plugins.
//
// A plugin is built with:
//
//	go build -buildmode=plugin -o plugins/my-plugin/plugin.so ./plugins/my-plugin
//
// and must export:
//
//	func Evaluate(context.Context, pluginapi.Request) (pluginapi.Result, error)
//
// The plugin and the worker must be built with the same Go toolchain and module
// dependency versions. Plugins run in the worker process and therefore must be
// trusted code.
package pluginapi

import (
	"context"
	"encoding/json"
)

const ProtocolVersion = "1"

type Request struct {
	ProtocolVersion string          `json:"protocol_version"`
	JobID           int64           `json:"job_id"`
	SubmissionID    int64           `json:"submission_id"`
	TaskID          int64           `json:"task_id"`
	SubmissionPath  string          `json:"submission_path"`
	Config          json.RawMessage `json:"config"`
}

type Result struct {
	ProtocolVersion string          `json:"protocol_version"`
	Score           float64         `json:"score"`
	Details         json.RawMessage `json:"details,omitempty"`
}

// Evaluator documents the callable plugin surface. Plugins expose Evaluate as a
// package-level function rather than exporting an Evaluator value.
type Evaluator interface {
	Evaluate(context.Context, Request) (Result, error)
}
