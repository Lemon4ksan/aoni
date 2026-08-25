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

	"github.com/andybalholm/brotli"
	kzstd "github.com/klauspost/compress/zstd"

	"github.com/lemon4ksan/aoni/internal/compress"
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

	// Prepare DCZ response payload
	enc, err := kzstd.NewWriter(nil, kzstd.WithEncoderDictRaw(1, schemaDict))
	if err != nil {
		t.Fatal(err)
	}

	compressedZstd := enc.EncodeAll(userJSON, nil)

	_ = enc.Close()

	dczStream := compress.WrapDCZHeader(compressedZstd, schemaHash)

	// Prepare DCB response payload
	var bBuf bytes.Buffer

	bw := brotli.NewWriter(&bBuf)
	_, _ = bw.Write(userJSON)
	_ = bw.Close()
	dcbStream := compress.WrapDCBHeader(bBuf.Bytes(), schemaHash)

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
			resp.Header.Set(dict.HeaderUseAsDictionary, `match="/api/*", id="schema-v1", ttl=86400`)

			return resp, nil
		})

		resp, err := pipe.Execute(context.Background(), httpReq, doer, pipeline.PipelineConfig{Decompress: true})
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
