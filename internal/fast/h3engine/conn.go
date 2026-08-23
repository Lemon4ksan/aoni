// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/internal/quic"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

const errCodeH3RequestCancelled = quic.StreamErrorCode(ErrCodeH3RequestCancelled)

var dataBufPool = generic.NewPool(func() *[]byte {
	b := make([]byte, 32768)
	return &b
})

// ClientConn manages HTTP/3 frame exchanges over a quic.Conn session (RFC 9114 §3, §4, §6 & §7).
type ClientConn struct {
	conn     *quic.Conn
	qpack    *QPACKCodec
	settings Settings

	closeOnce sync.Once
	closed    chan struct{}
}

// NewClientConn initializes an HTTP/3 client connection and opens control streams (RFC 9114 §3.2 & §6.2.1).
func NewClientConn(conn *quic.Conn, settings *Settings) (*ClientConn, error) {
	if settings == nil {
		settings = &Settings{
			MaxFieldSectionSize: 262144,
			EnableDatagrams:     true,
		}
	}

	cc := &ClientConn{
		conn:     conn,
		qpack:    NewQPACKCodec(),
		settings: *settings,
		closed:   make(chan struct{}),
	}

	if err := cc.setupControlStream(); err != nil {
		_ = conn.CloseWithError(quic.ApplicationErrorCode(ErrCodeH3NoError), "failed control stream setup")
		return nil, err
	}

	go cc.readUnidirectionalStreams()

	return cc, nil
}

func (cc *ClientConn) isClosed() bool {
	select {
	case <-cc.closed:
		return true
	default:
		return cc.conn.Context().Err() != nil
	}
}

func (cc *ClientConn) setupControlStream() error {
	str, err := cc.conn.OpenUniStream()
	if err != nil {
		return err
	}

	var buf []byte

	buf = quicvarint.Append(buf, StreamTypeControl)
	buf = append(buf, cc.settings.Encode()...)

	_, err = str.Write(buf)

	return err
}

func (cc *ClientConn) readUnidirectionalStreams() {
	for {
		str, err := cc.conn.AcceptUniStream(context.Background())
		if err != nil {
			return
		}

		go cc.handleUnidirectionalStream(str)
	}
}

func (cc *ClientConn) handleUnidirectionalStream(str *quic.ReceiveStream) {
	r := quicvarint.NewReader(str)

	streamType, err := quicvarint.Read(r)
	if err != nil {
		return
	}

	switch streamType {
	case StreamTypeControl:
		cc.readControlStream(r)
	case StreamTypeQPACKEncoder:
		cc.readQPACKEncoderStream(r)
	case StreamTypeQPACKDecoder:
		return
	default:
		// RFC 9114 §6.2: Unknown unidirectional stream types MUST be aborted or discarded.
		str.CancelRead(quic.StreamErrorCode(ErrCodeH3StreamCreationError))
	}
}

func (cc *ClientConn) readQPACKEncoderStream(r quicvarint.Reader) {
	buf := make([]byte, 4096)

	for {
		n, err := r.Read(buf)
		if n > 0 {
			_ = cc.qpack.WriteDecoderTable(buf[:n])
		}

		if err != nil {
			return
		}
	}
}

func (cc *ClientConn) readControlStream(r quicvarint.Reader) {
	firstFrame := true

	for {
		frameType, payloadLen, err := ReadFrameHeader(r)
		if err != nil {
			return
		}

		// RFC 9114 §6.2.1: The first frame on the control stream MUST be SETTINGS
		if firstFrame {
			if frameType != FrameTypeSettings {
				_ = cc.conn.CloseWithError(
					quic.ApplicationErrorCode(ErrCodeH3MissingSettings),
					"H3_MISSING_SETTINGS: first frame must be SETTINGS (RFC 9114 §6.2.1)",
				)

				return
			}

			firstFrame = false
		}

		switch frameType {
		case FrameTypeSettings:
			st, err := DecodeSettings(r, payloadLen)
			if err != nil {
				if errors.Is(err, ErrH3SettingsError) {
					_ = cc.conn.CloseWithError(
						quic.ApplicationErrorCode(ErrCodeH3SettingsError),
						"H3_SETTINGS_ERROR: reserved H2 setting ID (RFC 9114 §7.2.4.1)",
					)
				} else {
					_ = cc.conn.CloseWithError(
						quic.ApplicationErrorCode(ErrCodeH3SettingsError),
						"H3_SETTINGS_ERROR: invalid settings payload (RFC 9114 §7.2.4)",
					)
				}

				return
			}

			cc.settings = *st

		case FrameTypeGoAway:
			cc.handleGoAway(r, payloadLen)
			return

		default:
			if _, err := io.CopyN(io.Discard, r, int64(payloadLen)); err != nil { //nolint:gosec
				return
			}
		}
	}
}

func (cc *ClientConn) handleGoAway(r quicvarint.Reader, payloadLen uint64) {
	if payloadLen > 0 {
		_, _ = quicvarint.Read(r)
	}

	_ = cc.Close()
}

// Do executes a fasthttp.Request over a QUIC stream and populates fasthttp.Response and captured trailers.
func (cc *ClientConn) Do(
	ctx context.Context,
	req *fasthttp.Request,
	resp *fasthttp.Response,
	headerOrder []string,
) (map[string][]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	str, err := cc.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			str.CancelWrite(errCodeH3RequestCancelled)
			str.CancelRead(errCodeH3RequestCancelled)
		case <-done:
		}
	}()

	defer str.Close()

	if err := cc.sendRequest(str, req, headerOrder); err != nil {
		return nil, err
	}

	return cc.readResponse(str, resp)
}

func (cc *ClientConn) sendRequest(str *quic.Stream, req *fasthttp.Request, headerOrder []string) error {
	return cc.sendRequestTo(str, req, headerOrder)
}

func (cc *ClientConn) sendRequestTo(w io.Writer, req *fasthttp.Request, headerOrder []string) error {
	p := cc.qpack.AcquireEncoder()
	defer cc.qpack.ReleaseEncoder(p)

	headerBlock, err := cc.qpack.EncodeRequestHeadersPooled(p, req, headerOrder)
	if err != nil {
		return err
	}

	body := req.Body()

	headLen := quicvarint.Len(FrameTypeHeaders) + quicvarint.Len(uint64(len(headerBlock)))

	totalLen := headLen + len(headerBlock)
	if len(body) > 0 {
		totalLen += quicvarint.Len(FrameTypeData) + quicvarint.Len(uint64(len(body))) + len(body)
	}

	var (
		stackOut [8192]byte
		out      []byte
	)

	if totalLen <= len(stackOut) {
		out = stackOut[:0]
	} else {
		out = make([]byte, 0, totalLen)
	}

	out = appendHeadersHeader(out, uint64(len(headerBlock)))

	out = append(out, headerBlock...)
	if len(body) > 0 {
		out = appendDataHeader(out, uint64(len(body)))
		out = append(out, body...)
	}

	_, err = w.Write(out)

	return err
}

func (cc *ClientConn) readResponse(
	str *quic.Stream,
	resp *fasthttp.Response,
) (trailers map[string][]string, err error) {
	return cc.readResponseFrom(str, resp)
}

func (cc *ClientConn) readResponseFrom(
	reader io.Reader,
	resp *fasthttp.Response,
) (trailers map[string][]string, err error) {
	r := quicvarint.NewReader(reader)
	headersParsed := false

	var stackHeaderBuf [4096]byte

	for {
		frameType, payloadLen, err := ReadFrameHeader(r)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, err
		}

		switch frameType {
		case FrameTypeHeaders:
			var headerBlock []byte
			if payloadLen <= uint64(len(stackHeaderBuf)) {
				headerBlock = stackHeaderBuf[:payloadLen]
			} else {
				headerBlock = make([]byte, payloadLen)
			}

			if _, err := io.ReadFull(r, headerBlock); err != nil {
				return nil, err
			}

			if headersParsed {
				trailers, err = cc.qpack.DecodeResponseTrailers(headerBlock)
				if err != nil {
					return nil, err
				}
			} else {
				statusCode, err := cc.qpack.DecodeResponseHeaders(headerBlock, &resp.Header)
				if err != nil {
					return nil, err
				}

				if statusCode < 100 || statusCode >= 200 || statusCode == 101 {
					headersParsed = true
				}
			}

		case FrameTypeData:
			if !headersParsed {
				return nil, ErrFrameUnexpected
			}

			lr := io.LimitReader(r, int64(payloadLen)) //nolint:gosec
			bufPtr := dataBufPool.Get()
			buf := *bufPtr

			for {
				n, rErr := lr.Read(buf)
				if n > 0 {
					resp.AppendBody(buf[:n])
				}

				if rErr == io.EOF {
					break
				}

				if rErr != nil {
					dataBufPool.Put(bufPtr)
					return nil, rErr
				}
			}

			dataBufPool.Put(bufPtr)

		default:
			if _, err := io.CopyN(io.Discard, r, int64(payloadLen)); err != nil { //nolint:gosec
				return nil, err
			}
		}
	}

	return trailers, nil
}

// Close gracefully terminates the HTTP/3 client connection.
func (cc *ClientConn) Close() error {
	cc.closeOnce.Do(func() {
		close(cc.closed)
		_ = cc.conn.CloseWithError(0x100, "connection closed")
	})

	return nil
}
