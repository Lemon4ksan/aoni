// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v4"
)

// LimitDecoder wraps an existing decoder, ensuring that no more than maxBytes
// are read from the input stream during the decoding process. This provides
// protection against memory exhaustion attacks or oversized payloads.
func LimitDecoder(d Decoder, maxBytes int64) Decoder {
	return DecoderFunc(func(r io.Reader, target any) error {
		return d.Decode(io.LimitReader(r, maxBytes), target)
	})
}

// DecodeByContentType automatically parses the stream from r into target by selecting
// the most appropriate registered decoder based on the parsed MIME-type.
// Falls back to RawDecoder if the content type is unrecognized or raw binary is returned.
func DecodeByContentType(r io.Reader, contentType string, target any) error {
	// Simple manual parsing to avoid strict external mime dependencies
	mediaType := strings.Split(contentType, ";")[0]
	mediaType = strings.TrimSpace(strings.ToLower(mediaType))

	switch mediaType {
	case "application/json", "text/json":
		return JSONDecoder.Decode(r, target)
	case "application/xml", "text/xml":
		return XMLDecoder.Decode(r, target)
	case "application/x-yaml", "text/yaml", "text/x-yaml":
		return YAMLDecoder.Decode(r, target)
	default:
		return RawDecoder.Decode(r, target)
	}
}

// DecodeTo decodes the data from r using the provided decoder into a newly allocated T.
func DecodeTo[T any](r io.Reader, d Decoder) (T, error) {
	var target T
	if err := d.Decode(r, &target); err != nil {
		var zero T
		return zero, err
	}

	return target, nil
}

// Decoder decodes HTTP response bodies into target structures.
// Implementations include [JSONDecoder], [XMLDecoder], [YAMLDecoder], and [RawDecoder].
type Decoder interface {
	// Decode reads data from r and unmarshals it into target.
	Decode(r io.Reader, target any) error
}

// DecoderFunc adapts a function to the [Decoder] interface.
type DecoderFunc func(r io.Reader, target any) error

// Decode executes the underlying function to parse the reader into the target.
func (f DecoderFunc) Decode(r io.Reader, target any) error {
	return f(r, target)
}

// RawDecoder reads the entire response body into a byte slice.
// The target must be a *[]byte.
var RawDecoder Decoder = rawDecoder{}

type rawDecoder struct{}

func (rawDecoder) Decode(r io.Reader, target any) error {
	ptr, ok := target.(*[]byte)
	if !ok {
		return fmt.Errorf("aoni: RawDecoder requires *[]byte as output type, got %T", target)
	}

	buf := bufferPool.Get().(*bytes.Buffer)

	buf.Reset()
	defer bufferPool.Put(buf)

	bufPtr := bytePool.Get().(*[]byte)
	defer bytePool.Put(bufPtr)

	_, err := io.CopyBuffer(buf, r, *bufPtr)
	if err != nil {
		return err
	}

	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	*ptr = data

	return nil
}

// JSONDecoderConfig holds configuration parameters for creating a custom JSON decoder.
type JSONDecoderConfig struct {
	// DisallowUnknownFields causes the decoder to return an error if the destination
	// struct has no matching field for a key in the JSON payload.
	DisallowUnknownFields bool
	// UseNumber causes the decoder to unmarshal numbers into json.Number instead of float64.
	UseNumber bool
}

// NewJSONDecoder creates a custom [Decoder] configured with the specified parameters.
func NewJSONDecoder(cfg JSONDecoderConfig) Decoder {
	return DecoderFunc(func(r io.Reader, target any) error {
		dec := json.NewDecoder(r)
		if cfg.DisallowUnknownFields {
			dec.DisallowUnknownFields()
		}

		if cfg.UseNumber {
			dec.UseNumber()
		}

		return dec.Decode(target)
	})
}

// JSONDecoder parses the response body as JSON into target.
var JSONDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	return json.NewDecoder(r).Decode(target)
})

// XMLDecoder parses the response body as XML into target.
var XMLDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	return xml.NewDecoder(r).Decode(target)
})

// YAMLDecoder parses the response body as YAML into target.
var YAMLDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	return yaml.NewDecoder(r).Decode(target)
})

// AsRaw returns a [RequestModifier] that uses [RawDecoder].
func AsRaw() RequestModifier { return WithDecoder(RawDecoder) }

// AsJSON returns a [RequestModifier] that uses [JSONDecoder].
func AsJSON() RequestModifier { return WithDecoder(JSONDecoder) }

// AsXML returns a [RequestModifier] that uses [XMLDecoder].
func AsXML() RequestModifier { return WithDecoder(XMLDecoder) }

// AsYAML returns a [RequestModifier] that uses [YAMLDecoder].
func AsYAML() RequestModifier { return WithDecoder(YAMLDecoder) }

// DecodeJSON is a convenience helper to directly decode a JSON stream into T.
func DecodeJSON[T any](r io.Reader) (T, error) {
	return DecodeTo[T](r, JSONDecoder)
}

// DecodeXML is a convenience helper to directly decode an XML stream into T.
func DecodeXML[T any](r io.Reader) (T, error) {
	return DecodeTo[T](r, XMLDecoder)
}

// DecodeYAML is a convenience helper to directly decode a YAML stream into T.
func DecodeYAML[T any](r io.Reader) (T, error) {
	return DecodeTo[T](r, YAMLDecoder)
}
