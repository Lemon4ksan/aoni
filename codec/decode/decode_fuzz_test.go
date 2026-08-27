// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode_test

import (
	"bytes"
	"testing"

	"github.com/lemon4ksan/aoni/codec/decode"
)

type DecodeFuzzTarget struct {
	ID    int      `json:"id"    xml:"id"       yaml:"id"`
	Name  string   `json:"name"  xml:"name"     yaml:"name"`
	Tags  []string `json:"tags"  xml:"tags>tag" yaml:"tags"`
	Valid bool     `json:"valid" xml:"valid"    yaml:"valid"`
}

func FuzzDecoders(f *testing.F) {
	f.Add([]byte(`{"id":123,"name":"test","tags":["a","b"],"valid":true}`), "application/json")
	f.Add([]byte(`<DecodeFuzzTarget><id>123</id><name>test</name></DecodeFuzzTarget>`), "application/xml")
	f.Add([]byte("id: 123\nname: test\ntags:\n  - a\nvalid: true\n"), "application/x-yaml")
	f.Add([]byte{}, "application/json")
	f.Add([]byte(`not valid data format`), "application/octet-stream")

	f.Fuzz(func(t *testing.T, data []byte, contentType string) {
		var target DecodeFuzzTarget

		_ = decode.Payload(contentType, data, &target)

		r := bytes.NewReader(data)
		_, _ = decode.JSON[DecodeFuzzTarget](r)

		r.Reset(data)
		_, _ = decode.XML[DecodeFuzzTarget](r)

		r.Reset(data)
		_, _ = decode.YAML[DecodeFuzzTarget](r)

		r.Reset(data)
		_, _ = decode.Raw(r)
	})
}
