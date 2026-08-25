// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil/dict"
)

func TestPipeline_CompressionDictionaryLifecycle(t *testing.T) {
	dictStore := dict.NewStore()
	pipe := pipeline.New(pipeline.ClientDefaults{
		DictionaryStore: dictStore,
	}, pipeline.ClientFingerprint{})

	schemaDict := []byte(`{"fields":["id","name","email","role"]}`)
	schemaHash := sha256.Sum256(schemaDict)

	userJSON := []byte(
		`{"fields":["id","name","email","role"],"data":[{"id":1,"name":"Alice","email":"alice@example.com","role":"admin"}]}`,
	)

	// Pre-encoded DCZ response payload with dictionary schemaDict
	dczStream := []byte{
		0x5e, 0x2a, 0x4d, 0x18, 0x20, 0x0, 0x0, 0x0, 0xa8, 0x98, 0x96, 0x96, 0x59, 0x4d, 0xa5, 0xf5,
		0x8e, 0x58, 0x56, 0x46, 0xc2, 0x6f, 0xf8, 0x39, 0x7b, 0x4a, 0x14, 0x8f, 0x15, 0xc0, 0xc8, 0xe6,
		0xba, 0x6e, 0xd1, 0xff, 0xfb, 0xf1, 0x16, 0xf9, 0x28, 0xb5, 0x2f, 0xfd, 0x5, 0x0, 0x1, 0x15,
		0x2, 0x0, 0xb2, 0x3, 0xd, 0x14, 0xa0, 0xc9, 0x0, 0xb8, 0x78, 0x84, 0x1e, 0x81, 0x49, 0xc9,
		0x77, 0x55, 0xf1, 0xf3, 0x2, 0x29, 0x43, 0x0, 0xd, 0x3, 0x4b, 0xb1, 0xf4, 0x5c, 0xd6, 0xeb,
		0x34, 0x8, 0x30, 0x3c, 0xf2, 0x89, 0x9, 0x46, 0x87, 0xaf, 0xe9, 0x70, 0xa8, 0x6b, 0xf9, 0x53,
		0x13, 0xb8, 0x75, 0x55, 0x8, 0xf7, 0xc8, 0xab, 0x26, 0x3, 0x0, 0xdb, 0x20, 0xa1, 0x2c, 0x6c,
		0x59, 0x3d, 0x6a, 0xd, 0x67, 0x5f, 0xfa, 0xfa,
	}

	// Pre-encoded DCB response payload with dictionary schemaDict
	dcbStream := []byte{
		0xff, 0x44, 0x43, 0x42, 0xa8, 0x98, 0x96, 0x96, 0x59, 0x4d, 0xa5, 0xf5, 0x8e, 0x58, 0x56, 0x46,
		0xc2, 0x6f, 0xf8, 0x39, 0x7b, 0x4a, 0x14, 0x8f, 0x15, 0xc0, 0xc8, 0xe6, 0xba, 0x6e, 0xd1, 0xff,
		0xfb, 0xf1, 0x16, 0xf9, 0x1b, 0x72, 0x0, 0x0, 0x4, 0xfe, 0xf3, 0x96, 0xfa, 0xcb, 0x57, 0xd4,
		0x9f, 0x28, 0xf9, 0x76, 0xc1, 0x10, 0x6c, 0x28, 0xa2, 0xd1, 0xc9, 0x81, 0xc3, 0xb7, 0xdb, 0xa5,
		0x2f, 0x9, 0x1f, 0x80, 0x44, 0x90, 0xe7, 0xf0, 0x74, 0x3c, 0x42, 0x2d, 0x7b, 0xf9, 0xc1, 0xfd,
		0x17, 0x39, 0xb3, 0x63, 0x9c, 0xba, 0x7, 0x46, 0x55, 0x65, 0x5c, 0xbb, 0x84, 0x31, 0x78, 0x9,
		0xf2, 0x27, 0x6c, 0x25, 0x1c, 0xa2, 0xac, 0xd9, 0x2c, 0x23, 0x2a, 0xa7, 0xfc, 0x29, 0x18, 0x46,
		0x30, 0xc6, 0xa2, 0xc1, 0xf0, 0xe3, 0x3,
	}

	// Step 1: Initial request to register dictionary via Use-As-Dictionary
	t.Run("Stage 1 - Dictionary Registration", func(t *testing.T) {
		reqURL, _ := url.Parse("https://api.example.com/schema.json")
		httpReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, reqURL.String(), nil)

		doer := pipeline.DoerFunc(func(r *http.Request) (*http.Response, error) {
			resp := &http.Response{
				StatusCode:    http.StatusOK,
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
				Header:        make(http.Header),
				Body:          io.NopCloser(bytes.NewReader(schemaDict)),
				ContentLength: int64(len(schemaDict)),
				Request:       r,
			}
			resp.Header.Set("Use-As-Dictionary", `match="/api/*", id="schema-v1"`)
			resp.Header.Set("Content-Type", "application/json")

			return resp, nil
		})

		resp, err := pipe.Execute(context.Background(), httpReq, doer, pipeline.PipelineConfig{})
		if err != nil {
			t.Fatalf("pipeline execution failed: %v", err)
		}

		defer resp.Body.Close()

		// Verify dictionary was captured into store and matches /api/*
		targetMatchURL, _ := url.Parse("https://api.example.com/api/users")

		d, ok := dictStore.Match(targetMatchURL, "")
		if !ok || d == nil {
			t.Fatal("expected dictionary to be stored in cache and match /api/users, but not found")
		}

		if d.ID != "schema-v1" {
			t.Errorf("expected ID='schema-v1', got %q", d.ID)
		}
	})

	// Step 2: Request matching /api/* sending Available-Dictionary and decompressing DCZ
	t.Run("Stage 2 - DCZ Decompression", func(t *testing.T) {
		reqURL, _ := url.Parse("https://api.example.com/api/users")
		httpReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, reqURL.String(), nil)
		httpReq.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")

		var (
			receivedAvailDict      string
			receivedDictID         string
			receivedAcceptEncoding string
		)

		doer := pipeline.DoerFunc(func(r *http.Request) (*http.Response, error) {
			receivedAvailDict = r.Header.Get(dict.HeaderAvailableDictionary)
			receivedDictID = r.Header.Get(dict.HeaderDictionaryID)
			receivedAcceptEncoding = r.Header.Get("Accept-Encoding")

			resp := &http.Response{
				StatusCode:    http.StatusOK,
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
				Header:        make(http.Header),
				Body:          io.NopCloser(bytes.NewReader(dczStream)),
				ContentLength: int64(len(dczStream)),
				Request:       r,
			}
			resp.Header.Set("Content-Encoding", dict.ContentEncodingDCZ)

			return resp, nil
		})

		resp, err := pipe.Execute(context.Background(), httpReq, doer, pipeline.PipelineConfig{Decompress: true})
		if err != nil {
			t.Fatalf("pipeline execution failed: %v", err)
		}

		defer resp.Body.Close()

		// Verify request negotiation headers
		expectedAvail := dict.FormatAvailableDictionary(schemaHash)
		if receivedAvailDict != expectedAvail {
			t.Errorf("expected Available-Dictionary %q, got %q", expectedAvail, receivedAvailDict)
		}

		if receivedDictID != `"schema-v1"` {
			t.Errorf("expected Dictionary-ID %q, got %q", `"schema-v1"`, receivedDictID)
		}

		if receivedAcceptEncoding != "gzip, deflate, br, zstd, dcb, dcz" {
			t.Errorf("expected Accept-Encoding with dcb, dcz, got %q", receivedAcceptEncoding)
		}

		// Verify decompressed payload
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed reading response body: %v", err)
		}

		if !bytes.Equal(bodyBytes, userJSON) {
			t.Fatalf("decompressed body mismatch: got %q, want %q", bodyBytes, userJSON)
		}

		if !resp.Uncompressed {
			t.Error("expected resp.Uncompressed to be true")
		}
	})

	// Step 3: Request matching /api/* receiving DCB
	t.Run("Stage 3 - DCB Decompression", func(t *testing.T) {
		reqURL, _ := url.Parse("https://api.example.com/api/templates")
		httpReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, reqURL.String(), nil)
		httpReq.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")

		doer := pipeline.DoerFunc(func(r *http.Request) (*http.Response, error) {
			resp := &http.Response{
				StatusCode:    http.StatusOK,
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
				Header:        make(http.Header),
				Body:          io.NopCloser(bytes.NewReader(dcbStream)),
				ContentLength: int64(len(dcbStream)),
				Request:       r,
			}
			resp.Header.Set("Content-Encoding", dict.ContentEncodingDCB)

			return resp, nil
		})

		resp, err := pipe.Execute(context.Background(), httpReq, doer, pipeline.PipelineConfig{Decompress: true})
		if err != nil {
			t.Fatalf("pipeline execution failed: %v", err)
		}

		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed reading response body: %v", err)
		}

		if !bytes.Equal(bodyBytes, userJSON) {
			t.Fatalf("decompressed body mismatch: got %q, want %q", bodyBytes, userJSON)
		}
	})

	// Step 4: Insecure HTTP context rejection (RFC 9842 §8)
	t.Run("Stage 4 - Insecure Context Rejection", func(t *testing.T) {
		reqURL, _ := url.Parse("http://api.example.com/api/users")
		httpReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, reqURL.String(), nil)

		var receivedAvailDict string

		doer := pipeline.DoerFunc(func(r *http.Request) (*http.Response, error) {
			receivedAvailDict = r.Header.Get(dict.HeaderAvailableDictionary)

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("ok"))),
				Request:    r,
			}, nil
		})

		resp, err := pipe.Execute(context.Background(), httpReq, doer, pipeline.PipelineConfig{})
		if err != nil {
			t.Fatal(err)
		}

		_ = resp.Body.Close()

		if receivedAvailDict != "" {
			t.Errorf("expected no Available-Dictionary header on plain HTTP, got %q", receivedAvailDict)
		}
	})
}
