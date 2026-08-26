// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"context"
	"time"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// WithContext constructs an [RequestModifier] updating the execution context associated with the request.
func WithContext(ctx context.Context) RequestModifier {
	return Custom(func(req Request) {
		req.SetContext(ctx)
	})
}

// WithTimeout constructs an [RequestModifier] attaching a deadline timeout to the request context.
func WithTimeout(d time.Duration) RequestModifier {
	return Custom(func(req Request) {
		ctx, cancel := context.WithTimeout(req.Context(), d) //nolint:gosec
		req.SetContext(ctx)
		getOrInitRequestConfig(req).RequestTimeoutCancel = cancel
	})
}

// WithPipeline constructs an [RequestModifier] overriding execution pipeline configurations for the request.
func WithPipeline(pipe pipeline.PipelineConfig) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Pipeline = &pipe
	})
}

// PhaseID identifies fixed transaction execution phases.
type PhaseID = pipeline.PhaseID

const (
	PhasePrep        = pipeline.PhasePrep
	PhaseCacheLookup = pipeline.PhaseCacheLookup
	PhaseDispatch    = pipeline.PhaseDispatch
	PhaseDecompress  = pipeline.PhaseDecompress
	PhaseWAF         = pipeline.PhaseWAF
	PhaseValidate    = pipeline.PhaseValidate
	PhaseCacheSave   = pipeline.PhaseCacheSave
)

// WithUnsafePhaseOrder sets a custom phase order for the pipeline.
func WithUnsafePhaseOrder(phases ...PhaseID) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).UnsafePhaseOrder = phases
	})
}

// WithUnsafeDisableFlags allows to disable pipeline phases instantly (by clearing bits in 1 CPU cycle).
// Example: mod.WithUnsafeDisableFlags(pipeline.FlagChallenge | pipeline.FlagCache)
func WithUnsafeDisableFlags(flags uint32) RequestModifier {
	return Custom(func(req Request) {
		cfg := getOrInitRequestConfig(req)
		cfg.DisabledFlags |= flags
	})
}

// WithUnsafeHook inserts a zero-allocation hook before the specified pipeline phase.
func WithUnsafeHook(phase pipeline.PhaseID, hook pipeline.UnsafeHook) RequestModifier {
	return Custom(func(req Request) {
		cfg := getOrInitRequestConfig(req)
		if cfg.UnsafeHooks == nil {
			cfg.UnsafeHooks = make(map[pipeline.PhaseID][]pipeline.UnsafeHook)
		}

		cfg.UnsafeHooks[phase] = append(cfg.UnsafeHooks[phase], hook)
	})
}

// WithRedact constructs an [RequestModifier] configuring header and key redaction rules for logging.
func WithRedact(cfg pipeline.RedactConfig) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Redact = &cfg
	})
}

// WithConnMetadata constructs an [RequestModifier] associating custom key-value metadata with the request connection.
func WithConnMetadata(key string, val any) RequestModifier {
	return Custom(func(req Request) {
		cfg := getOrInitRequestConfig(req)
		if cfg.Metadata == nil {
			cfg.Metadata = make(map[string]any)
		}

		cfg.Metadata[key] = val
	})
}

// WithForceContentType constructs an [RequestModifier] forcing response decoding via a specific MIME type.
func WithForceContentType(mime string) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ForceContentType = mime
	})
}

// WithErrorModel constructs an [RequestModifier] assigning a target struct pointer for non-2xx API error response unmarshaling.
func WithErrorModel(model any) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ErrorModel = model
	})
}

// WithDecoder constructs an [RequestModifier] overriding the response decoder implementation for the request.
func WithDecoder(d core.ResponseDecoder) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Decoder = d
	})
}

// WithUploadProgress constructs an [RequestModifier] registering an upload progress tracking callback.
func WithUploadProgress(progress core.ProgressFunc) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).UploadProgress = progress
	})
}

// WithDownloadProgress constructs an [RequestModifier] registering a download progress tracking callback.
func WithDownloadProgress(progress core.ProgressFunc) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).DownloadProgress = progress
	})
}

// WithCaptureResponse constructs an [RequestModifier] capturing a reference pointer to the raw [*http.Response].
func WithCaptureResponse(target any) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Capturer = target
	})
}

// WithoutBaseResponse constructs an [RequestModifier] disabling base response allocation for maximum RPS.
func WithoutBaseResponse() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).DisableBaseResponse = true
	})
}
