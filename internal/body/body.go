// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package body

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/core"
)

var nullJSONBytes = []byte("null")

// Body encapsulates a validated, serialized request body ready for transmission.
type Body struct {
	Bytes       []byte
	Reader      io.Reader
	ContentType string
	GetBody     func() (io.ReadCloser, error)
}

// IsEmpty reports whether the payload contains no readable body stream.
func (p Body) IsEmpty() bool {
	return len(p.Bytes) == 0 && p.Reader == nil
}

// HasContentType reports whether the payload explicitly declares a MIME content-type.
func (p Body) HasContentType() bool {
	return p.ContentType != ""
}

func bodyFromBytes(b []byte, contentType string) Body {
	return Body{
		Bytes:       b,
		Reader:      bytes.NewReader(b),
		ContentType: contentType,
		GetBody: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		},
	}
}

func bodyFromString(s, contentType string) Body {
	b := []byte(s)

	return Body{
		Bytes:       b,
		Reader:      strings.NewReader(s),
		ContentType: contentType,
		GetBody: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		},
	}
}

// ValidateAndMarshal inspects arbitrary payload inputs and transforms them into a serialized [Body].
//
// Conforms to Protocol-Oriented Programming principles:
//   - Inspects [core.BodyProvider] to obtain custom streams.
//   - Inspects [core.ContentTyper] to discover explicit MIME content types.
//   - Inspects [core.BodyRewinder] to preserve reproducible stream reads for retries/hedging.
//   - Inspects [io.WriterTo] for high-efficiency buffer serialization.
func ValidateAndMarshal(body any) (Body, error) {
	if body == nil {
		return Body{}, nil
	}

	var payload Body

	switch v := body.(type) {
	case core.RequestModifier:
		return Body{}, core.ErrModifierAsBody

	case core.BodyProvider:
		r, err := v.Body()
		if err != nil {
			return Body{}, fmt.Errorf("aoni: failed to obtain body from provider: %w", err)
		}

		payload = Body{Reader: r}

	case *strings.Reader:
		snapshot := *v
		payload = Body{
			Reader: v,
			GetBody: func() (io.ReadCloser, error) {
				r := snapshot
				return io.NopCloser(&r), nil
			},
		}

	case *bytes.Reader:
		snapshot := *v
		payload = Body{
			Reader: v,
			GetBody: func() (io.ReadCloser, error) {
				r := snapshot
				return io.NopCloser(&r), nil
			},
		}

	case *bytes.Buffer:
		buf := v.Bytes()
		payload = Body{
			Bytes:  buf,
			Reader: v,
			GetBody: func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(buf)), nil
			},
		}

	case io.Reader:
		payload = Body{Reader: v}

	case io.WriterTo:
		var buf bytes.Buffer
		if _, err := v.WriteTo(&buf); err != nil {
			return Body{}, fmt.Errorf("aoni: failed to write payload via io.WriterTo: %w", err)
		}

		payload = bodyFromBytes(buf.Bytes(), "")

	case []byte:
		payload = bodyFromBytes(v, "")

	case string:
		payload = bodyFromString(v, "")

	case url.Values:
		payload = bodyFromString(v.Encode(), "application/x-www-form-urlencoded")

	case *url.Values:
		if v == nil {
			return Body{}, nil
		}

		payload = bodyFromString(v.Encode(), "application/x-www-form-urlencoded")

	case proto.Message:
		if v == nil || (reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil()) {
			return Body{}, nil
		}

		bodyBytes, err := proto.Marshal(v)
		if err != nil {
			return Body{}, fmt.Errorf("aoni: failed to marshal protobuf payload: %w", err)
		}

		payload = bodyFromBytes(bodyBytes, "application/x-protobuf")

	default:
		bodyBytes, err := json.Marshal(v)
		if err != nil {
			return Body{}, fmt.Errorf("aoni: failed to marshal JSON payload: %w", err)
		}

		if bytes.Equal(bodyBytes, nullJSONBytes) {
			return Body{}, nil
		}

		payload = bodyFromBytes(bodyBytes, "application/json")
	}

	// Protocol capabilities: ContentTyper & BodyRewinder
	if ct, ok := body.(core.ContentTyper); ok {
		if mime := ct.ContentType(); mime != "" {
			payload.ContentType = mime
		}
	}

	if rewinder, ok := body.(core.BodyRewinder); ok {
		payload.GetBody = rewinder.GetBody
	}

	return payload, nil
}
