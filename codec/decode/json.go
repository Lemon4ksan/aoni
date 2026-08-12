// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"encoding/json"
	stdio "io"
)

// JSONDecoderConfig configures parsing options for JSON response streams.
type JSONDecoderConfig struct {
	DisallowUnknownFields bool
	UseNumber             bool
}

type customJSONDecoder struct {
	cfg JSONDecoderConfig
}

func (d customJSONDecoder) Decode(reader stdio.Reader, target any) error {
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

type jsonDecoder struct{}

func (jsonDecoder) Decode(reader stdio.Reader, target any) error {
	return json.NewDecoder(reader).Decode(target)
}
