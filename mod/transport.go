// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"context"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/foundation/pipeline"
)

// WithContext constructs an [aoni.RequestModifier] updating the execution context associated with the request.
func WithContext(ctx context.Context) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		req.SetContext(ctx)
	})
}

// WithTimeout constructs an [aoni.RequestModifier] attaching a deadline timeout to the request context.
func WithTimeout(d time.Duration) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), d) //nolint:gosec
		req.SetContext(ctx)
		aoni.GetOrInitRequestConfig(req).RequestTimeoutCancel = cancel
	})
}

// WithPipeline constructs an [aoni.RequestModifier] overriding execution pipeline configurations for the request.
func WithPipeline(pipe aoni.PipelineConfig) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		p := pipe.ToInternal()
		aoni.GetOrInitRequestConfig(req).Pipeline = &p
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
func WithUnsafePhaseOrder(phases ...PhaseID) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).UnsafePhaseOrder = phases
	})
}

// WithUnsafeDisableFlags allows to disable pipeline phases instantly (by clearing bits in 1 CPU cycle).
// Example: mod.WithUnsafeDisableFlags(pipeline.FlagChallenge | pipeline.FlagCache)
func WithUnsafeDisableFlags(flags uint32) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		cfg.DisabledFlags |= flags
	})
}

// WithUnsafeHook inserts a zero-allocation hook before the specified pipeline phase.
func WithUnsafeHook(phase pipeline.PhaseID, hook pipeline.UnsafeHook) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.UnsafeHooks == nil {
			cfg.UnsafeHooks = make(map[pipeline.PhaseID][]pipeline.UnsafeHook)
		}

		cfg.UnsafeHooks[phase] = append(cfg.UnsafeHooks[phase], hook)
	})
}

// WithRedact constructs an [aoni.RequestModifier] configuring header and key redaction rules for logging.
func WithRedact(cfg aoni.RedactConfig) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		r := pipeline.RedactConfig{
			Headers:          cfg.Headers,
			HeadersToRedact:  cfg.HeadersToRedact,
			JSONKeysToRedact: cfg.JSONKeysToRedact,
		}
		aoni.GetOrInitRequestConfig(req).Redact = &r
	})
}

// WithConnMetadata constructs an [aoni.RequestModifier] associating custom key-value metadata with the request connection.
func WithConnMetadata(key string, val any) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.Metadata == nil {
			cfg.Metadata = make(map[string]any)
		}

		cfg.Metadata[key] = val
	})
}

// WithForceContentType constructs an [aoni.RequestModifier] forcing response decoding via a specific MIME type.
func WithForceContentType(mime string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ForceContentType = mime
	})
}

// WithErrorModel constructs an [aoni.RequestModifier] assigning a target struct pointer for non-2xx API error response unmarshaling.
func WithErrorModel(model any) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ErrorModel = model
	})
}

// WithDecoder constructs an [aoni.RequestModifier] overriding the response decoder implementation for the request.
func WithDecoder(d aoni.ResponseDecoder) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Decoder = d
	})
}

// WithUploadProgress constructs an [aoni.RequestModifier] registering an upload progress tracking callback.
func WithUploadProgress(progress aoni.ProgressFunc) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).UploadProgress = progress
	})
}

// WithDownloadProgress constructs an [aoni.RequestModifier] registering a download progress tracking callback.
func WithDownloadProgress(progress aoni.ProgressFunc) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DownloadProgress = progress
	})
}

// WithCaptureResponse constructs an [aoni.RequestModifier] capturing a reference pointer to the raw [*http.Response].
func WithCaptureResponse(target any) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Capturer = target
	})
}

// WithoutBaseResponse constructs an [aoni.RequestModifier] disabling base response allocation for maximum RPS.
func WithoutBaseResponse() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DisableBaseResponse = true
	})
}
