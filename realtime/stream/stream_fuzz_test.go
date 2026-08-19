// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stream_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/realtime/stream"
)

// FuzzSSEStream tests Server-Sent Events line framing, event dispatch, and field parsers against arbitrary byte chunks.
func FuzzSSEStream(f *testing.F) {
	f.Add([]byte("event: message\ndata: {\"text\":\"hello\"}\nid: 1\nretry: 1000\n\n"))
	f.Add([]byte(": this is a comment\ndata: first line\ndata: second line\n\n"))
	f.Add([]byte("data: [DONE]\n\n"))
	f.Add([]byte(""))
	f.Add([]byte("\r\n\r\n\r\n"))
	f.Add([]byte("data: unclosed"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(data)),
			Header:     make(http.Header),
		}

		st := stream.NewStream(resp)
		defer st.Close()

		out, errs := stream.ParseSSE[stream.SSEEvent](ctx, st)

		for out != nil || errs != nil {
			select {
			case _, ok := <-out:
				if !ok {
					out = nil
				}
			case _, ok := <-errs:
				if !ok {
					errs = nil
				}
			case <-ctx.Done():
				return
			}
		}
	})
}

// FuzzNDJSONStream tests newline-delimited JSON stream decoding against arbitrary byte chunks.
func FuzzNDJSONStream(f *testing.F) {
	f.Add([]byte("{\"id\":1,\"name\":\"alpha\"}\n{\"id\":2,\"name\":\"beta\"}\n"))
	f.Add([]byte("{\"id\": 1}\r\n{\"id\": 2}\r\n"))
	f.Add([]byte(""))
	f.Add([]byte("not a json\n{\"valid\":true}\n"))

	type Item struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(data)),
			Header:     make(http.Header),
		}

		st := stream.NewStream(resp)
		defer st.Close()

		out, errs := stream.GetNDJSON[Item](ctx, st)

		for out != nil || errs != nil {
			select {
			case _, ok := <-out:
				if !ok {
					out = nil
				}
			case _, ok := <-errs:
				if !ok {
					errs = nil
				}
			case <-ctx.Done():
				return
			}
		}
	})
}
