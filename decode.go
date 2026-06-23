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

	"go.yaml.in/yaml/v4"
)

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
