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

// Decoder defines the contract for decoding response bodies into target structures.
// Implementations of this interface (such as [JSONDecoder] or [XMLDecoder]) are
// used by [Client] to deserialize HTTP response payloads.
type Decoder interface {
	// Decode reads data from the reader and unmarshals it into the target destination.
	// It returns an error if reading or decoding fails, or if target is incompatible.
	Decode(r io.Reader, target any) error
}

// DecoderFunc adapts a plain function to satisfy the [Decoder] interface.
// This is useful for defining inline or lightweight custom decoders.
type DecoderFunc func(r io.Reader, target any) error

// Decode executes the underlying function to parse the reader into the target.
func (f DecoderFunc) Decode(r io.Reader, target any) error {
	return f(r, target)
}

// RawDecoder reads the entire response body and returns it as a raw byte slice.
// The target argument must be a non-nil pointer to a byte slice (*[]byte).
// It returns an error if target is not a pointer to a byte slice or if the read fails.
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

// JSONDecoder parses the response body using the standard encoding/json package.
// The target argument must be a pointer to the destination structure.
var JSONDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	return json.NewDecoder(r).Decode(target)
})

// XMLDecoder parses the response body using the standard encoding/xml package.
// The target argument must be a pointer to the destination structure.
var XMLDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	return xml.NewDecoder(r).Decode(target)
})

// YAMLDecoder parses the response body using the gopkg.in/yaml.v3 package.
// The target argument must be a pointer to the destination structure.
var YAMLDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	return yaml.NewDecoder(r).Decode(target)
})

// AsRaw returns a [RequestModifier] that configures the client to use [RawDecoder].
func AsRaw() RequestModifier { return WithDecoder(RawDecoder) }

// AsJSON returns a [RequestModifier] that configures the client to use [JSONDecoder].
// Clients use this modifier by default when no other decoder is specified.
func AsJSON() RequestModifier { return WithDecoder(JSONDecoder) }

// AsXML returns a [RequestModifier] that configures the client to use [XMLDecoder].
func AsXML() RequestModifier { return WithDecoder(XMLDecoder) }

// AsYAML returns a [RequestModifier] that configures the client to use [YAMLDecoder].
func AsYAML() RequestModifier { return WithDecoder(YAMLDecoder) }
