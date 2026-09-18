// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"io"

	"github.com/lemon4ksan/foundation/codec/json"

	"github.com/lemon4ksan/aoni/internal/arena"
)

// JSONDecoderConfig configures parsing options for JSON response streams.
type JSONDecoderConfig struct {
	// DisallowUnknownFields causes the decoder to return an error if the input contains keys that do not match fields in the target struct.
	DisallowUnknownFields bool
	// UseNumber causes the decoder to unmarshal numbers into Interface{} as a [json.Number] instead of a float64.
	UseNumber bool
	// NoCopy avoids string allocations by referencing string fields directly from the underlying byte payload.
	NoCopy bool
}

// customJSONDecoder applies custom decoding flags (DisallowUnknownFields, UseNumber, NoCopy) during JSON stream parsing.
type customJSONDecoder struct {
	cfg JSONDecoderConfig
}

func (d customJSONDecoder) Decode(reader io.Reader, target any) error {
	cfg := json.DecoderConfig{
		DisallowUnknownFields: d.cfg.DisallowUnknownFields,
		UseNumber:             d.cfg.UseNumber,
		NoCopy:                d.cfg.NoCopy,
	}

	if data, _, ok := InspectBytes(reader); ok {
		data = StripBOMBytes(data)
		if len(data) == 0 {
			return nil
		}

		return json.UnmarshalWithConfig(data, target, cfg)
	}

	dec := json.NewDecoder(StripBOM(reader))
	if d.cfg.DisallowUnknownFields {
		dec.DisallowUnknownFields()
	}

	if d.cfg.UseNumber {
		dec.UseNumber()
	}

	return dec.Decode(target)
}

// NewJSONDecoder creates a custom JSON [Decoder] with the specified configuration parameters.
func NewJSONDecoder(cfg JSONDecoderConfig) Decoder {
	return customJSONDecoder{cfg: cfg}
}

// jsonDecoder parses response payload streams as standard JSON using [json.Unmarshal] or [json.NewDecoder].
type jsonDecoder struct{}

func (jsonDecoder) Decode(reader io.Reader, target any) error {
	if data, _, ok := InspectBytes(reader); ok {
		data = StripBOMBytes(data)
		if len(data) == 0 {
			return nil
		}

		return json.Unmarshal(data, target)
	}

	return json.NewDecoder(StripBOM(reader)).Decode(target)
}

// JSONArena performs JSON deserialization into an arena-allocated struct.
//
// To completely bypass Go's garbage collector when parsing API responses, it:
//  1. Allocates the target struct `T` inside the provided [arena.Arena] (a pre-allocated byte slice).
//  2. Uses [json.UnmarshalNoCopy] when unmarshaling, forcing all string fields (e.g. `Name string`)
//     to point directly into the raw JSON payload bytes using `unsafe.String` rather than allocating new strings.
//
// DANGER: DO NOT RETAIN POINTERS OR STRINGS
// The returned pointer AND all strings inside the struct reference ephemeral pool memory.
// They will cause memory corruption if accessed after the Arena is released.
func JSONArena[T any](reader io.Reader, a *arena.Arena) (*T, error) {
	target := arena.Alloc[T](a)

	if data, _, ok := InspectBytes(reader); ok {
		data = StripBOMBytes(data)
		if len(data) == 0 {
			return target, nil
		}

		err := json.UnmarshalNoCopy(data, target)
		if err != nil {
			return nil, err
		}

		return target, nil
	}

	data, err := a.ReadAll(StripBOM(reader))
	if err != nil {
		return nil, err
	}

	data = StripBOMBytes(data)
	if len(data) == 0 {
		return target, nil
	}

	err = json.UnmarshalWithConfig(data, target, json.DecoderConfig{NoCopy: true})
	if err != nil {
		return nil, err
	}

	return target, nil
}
