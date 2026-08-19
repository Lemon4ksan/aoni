// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"bytes"
	"context"
	"errors"
	stdio "io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/silicon/offheap"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/io"
)

// WithMultipart constructs an [aoni.RequestModifier] building an in-memory multipart/form-data request body.
func WithMultipart(fields map[string]string, files map[string]stdio.Reader) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: aoni.ModCustom,
		Fn: func(req aoni.Request) {
			offBuf, err := offheap.NewBuffer(64 * 1024)

			var (
				body     stdio.Writer = &bytes.Buffer{}
				getBytes              = func() []byte {
					return body.(*bytes.Buffer).Bytes()
				}
			)

			if err == nil {
				defer offBuf.Release()

				body = offBuf
				getBytes = func() []byte { return slices.Clone(offBuf.Bytes()) }
			}

			writer := multipart.NewWriter(body)

			if cfg := aoni.GetOrInitRequestConfig(req); cfg.MultipartBoundary != "" {
				_ = writer.SetBoundary(cfg.MultipartBoundary)
			}

			for k, v := range fields {
				if err := writer.WriteField(k, v); err != nil {
					aoni.GetOrInitRequestConfig(req).BodyError = err
					return
				}
			}

			for key, r := range files {
				part, err := writer.CreateFormFile(key, key)
				if err != nil {
					aoni.GetOrInitRequestConfig(req).BodyError = err
					return
				}

				if _, err = io.CopyZeroAlloc(part, r); err != nil {
					aoni.GetOrInitRequestConfig(req).BodyError = err
					return
				}
			}

			if err := writer.Close(); err != nil {
				aoni.GetOrInitRequestConfig(req).BodyError = err
				return
			}

			req.SetBodyBytes(getBytes())
			req.SetHeader("Content-Type", writer.FormDataContentType())
		},
	}
}

type MultipartField struct {
	Name        string
	Value       string
	Filename    string
	ContentType string
	Reader      stdio.Reader
}

// WithMultipartFields accepts an ordered slice of form fields with support for duplicate names (RFC 7578 Section 5.2)
func WithMultipartFields(fields []MultipartField) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: aoni.ModCustom,
		Fn: func(req aoni.Request) {
			offBuf, err := offheap.NewBuffer(64 * 1024)

			var (
				body     stdio.Writer = &bytes.Buffer{}
				getBytes              = func() []byte {
					return body.(*bytes.Buffer).Bytes()
				}
			)

			if err == nil {
				defer offBuf.Release()

				body = offBuf
				getBytes = func() []byte { return slices.Clone(offBuf.Bytes()) }
			}

			writer := multipart.NewWriter(body)

			if cfg := aoni.GetOrInitRequestConfig(req); cfg.MultipartBoundary != "" {
				_ = writer.SetBoundary(cfg.MultipartBoundary)
			}

			for _, f := range fields {
				if f.Reader != nil || f.Filename != "" {
					ct := f.ContentType
					if ct == "" {
						ct = "application/octet-stream"
					}

					part, err := createFormFileHeader(writer, f.Name, f.Filename, ct)
					if err != nil {
						aoni.GetOrInitRequestConfig(req).BodyError = err
						return
					}

					if f.Reader != nil {
						if _, err = io.CopyZeroAlloc(part, f.Reader); err != nil {
							aoni.GetOrInitRequestConfig(req).BodyError = err
							return
						}
					}
				} else {
					if err := writer.WriteField(f.Name, f.Value); err != nil {
						aoni.GetOrInitRequestConfig(req).BodyError = err
						return
					}
				}
			}

			if err := writer.Close(); err != nil {
				aoni.GetOrInitRequestConfig(req).BodyError = err
				return
			}

			req.SetBodyBytes(getBytes())
			req.SetHeader("Content-Type", writer.FormDataContentType())
		},
	}
}

// WithStreamingMultipart constructs an [aoni.RequestModifier] streaming multipart/form-data via an asynchronous pipe without in-memory buffering.
func WithStreamingMultipart(fields map[string]string, files map[string]stdio.Reader) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: aoni.ModCustom,
		Fn: func(req aoni.Request) {
			pr, pw := stdio.Pipe()

			writer := multipart.NewWriter(pw)
			if cfg := aoni.GetOrInitRequestConfig(req); cfg.MultipartBoundary != "" {
				_ = writer.SetBoundary(cfg.MultipartBoundary)
			}

			ctx := req.Context()
			go streamMultipartPayload(ctx, pw, writer, fields, files)

			req.SetBodyStream(pr, -1)
			req.SetHeader("Content-Type", writer.FormDataContentType())
		},
	}
}

// streamMultipartPayload continuously encodes and streams multipart fields and files through pw.
func streamMultipartPayload(
	ctx context.Context,
	pw *stdio.PipeWriter,
	writer *multipart.Writer,
	fields map[string]string,
	files map[string]stdio.Reader,
) {
	defer pw.Close()
	defer writer.Close()

	for k, v := range fields {
		select {
		case <-ctx.Done():
			_ = pw.CloseWithError(ctx.Err())
			return
		default:
			_ = writer.WriteField(k, v)
		}
	}

	for key, r := range files {
		select {
		case <-ctx.Done():
			_ = pw.CloseWithError(ctx.Err())
			return
		default:
			contentType, streamReader := detectMIMEAndReader(r)

			part, err := createFormFileHeader(writer, key, key, contentType)
			if err == nil {
				_, _ = io.CopyZeroAlloc(part, streamReader)
			}
		}
	}
}

// detectMIMEAndReader peeks at the first 512 bytes of r on the stack to sniff Content-Type.
func detectMIMEAndReader(r stdio.Reader) (string, stdio.Reader) {
	var buf [512]byte

	n, err := stdio.ReadFull(r, buf[:])
	if n > 0 {
		contentType := http.DetectContentType(buf[:n])
		reader := stdio.MultiReader(bytes.NewReader(buf[:n]), r)

		return contentType, reader
	}

	if err != nil && !errors.Is(err, stdio.EOF) && !errors.Is(err, stdio.ErrUnexpectedEOF) {
		return "application/octet-stream", r
	}

	return "application/octet-stream", r
}

// createFormFileHeader builds a multipart MIME header with proper Content-Disposition and Content-Type.
func createFormFileHeader(w *multipart.Writer, fieldname, filename, contentType string) (stdio.Writer, error) {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		"form-data; name=\""+escapeQuotes(fieldname)+"\"; filename=\""+escapeQuotes(filename)+"\"")

	if contentType != "" {
		h.Set("Content-Type", contentType)
	} else {
		h.Set("Content-Type", "application/octet-stream")
	}

	return w.CreatePart(h)
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// escapeQuotes escapes backslashes and double quotes for MIME header parameter values.
func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}
