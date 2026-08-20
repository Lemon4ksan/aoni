// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"encoding/json"
	"io"
)

// JSONDecoderConfig configures parsing options for JSON response streams.
type JSONDecoderConfig struct {
	// DisallowUnknownFields causes the decoder to return an error if the input contains keys that do not match fields in the target struct.
	DisallowUnknownFields bool
	// UseNumber causes the decoder to unmarshal numbers into Interface{} as a [json.Number] instead of a float64.
	UseNumber bool
}

// customJSONDecoder applies custom decoding flags (DisallowUnknownFields, UseNumber) during JSON stream parsing.
type customJSONDecoder struct {
	cfg JSONDecoderConfig
}

func (d customJSONDecoder) Decode(reader io.Reader, target any) error {
	dec := json.NewDecoder(reader)
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

// jsonDecoder parses response payload streams as standard JSON using [json.NewDecoder].
type jsonDecoder struct{}

func (jsonDecoder) Decode(reader io.Reader, target any) error {
	return json.NewDecoder(reader).Decode(target)
}
