// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"io"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/internal/compress"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

func decompressFastResponse(resp *h1engine.Response) bool {
	encodingBytes := resp.Header.ContentEncoding()
	if len(encodingBytes) == 0 {
		return false
	}

	enforceContentLengthTruncation(resp)

	body := resp.Body()
	if len(body) == 0 {
		return false
	}

	decompressed, err := compress.Decompress(bytesconv.B2S(encodingBytes), body, nil)
	if err == nil && len(decompressed) > 0 {
		resp.SetBody(decompressed)
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")

		return true
	}

	return false
}

func enforceContentLengthTruncation(resp *h1engine.Response) {
	if resp == nil {
		return
	}

	cl := resp.Header.ContentLength()
	if cl < 0 {
		return
	}

	if !resp.IsBodyStream() {
		body := resp.Body()
		if len(body) > cl {
			resp.SetBody(body[:cl])
		}

		return
	}

	if stream := resp.BodyStream(); stream != nil {
		resp.SetBodyStream(io.LimitReader(stream, int64(cl)), cl)
	}
}
