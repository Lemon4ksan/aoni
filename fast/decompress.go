// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"io"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/internal/compress"
)

func decompressFastResponse(resp *fasthttp.Response) bool {
	enforceContentLengthTruncation(resp)

	encodingBytes := resp.Header.ContentEncoding()
	if len(encodingBytes) == 0 {
		return false
	}

	body := resp.Body()
	if len(body) == 0 {
		return false
	}

	var (
		decompressed []byte
		err          error
	)

	switch {
	case bytesconv.ContainsFoldASCII(encodingBytes, "gzip"):
		decompressed, err = resp.BodyGunzip()

	case bytesconv.ContainsFoldASCII(encodingBytes, "br"):
		decompressed, err = compress.Unbrotli(body, nil)

	case bytesconv.ContainsFoldASCII(encodingBytes, "zstd"):
		decompressed, err = compress.Unzstd(body, nil)
	}

	if err == nil && len(decompressed) > 0 {
		resp.SetBody(decompressed)
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")

		return true
	}

	return false
}

func enforceContentLengthTruncation(resp *fasthttp.Response) {
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
