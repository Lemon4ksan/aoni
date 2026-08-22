// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"bytes"
	"io"

	"gopkg.in/yaml.v3"
)

// YAMLDecoderConfig configures parsing options for YAML response streams.
type YAMLDecoderConfig struct {
	// KnownFields causes the decoder to return an error if the input contains keys that do not match fields in the target struct.
	KnownFields bool
}

// customYAMLDecoder applies custom decoding flags during YAML stream parsing.
type customYAMLDecoder struct {
	cfg YAMLDecoderConfig
}

func (d customYAMLDecoder) Decode(reader io.Reader, target any) error {
	if data, _, ok := InspectBytes(reader); ok {
		if len(data) == 0 {
			return nil
		}

		dec := yaml.NewDecoder(bytes.NewReader(StripBOMBytes(data)))
		dec.KnownFields(d.cfg.KnownFields)

		return dec.Decode(target)
	}

	dec := yaml.NewDecoder(StripBOM(reader))
	dec.KnownFields(d.cfg.KnownFields)

	return dec.Decode(target)
}

// NewYAMLDecoder creates a custom YAML [Decoder] with the specified configuration parameters.
func NewYAMLDecoder(cfg YAMLDecoderConfig) Decoder {
	return customYAMLDecoder{cfg: cfg}
}

// yamlDecoder unmarshals YAML response streams into Go target structs.
type yamlDecoder struct{}

func (yamlDecoder) Decode(reader io.Reader, target any) error {
	if data, _, ok := InspectBytes(reader); ok {
		if len(data) == 0 {
			return nil
		}

		return yaml.Unmarshal(StripBOMBytes(data), target)
	}

	return yaml.NewDecoder(StripBOM(reader)).Decode(target)
}
