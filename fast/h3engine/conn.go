// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/valyala/fasthttp"
)

const errCodeH3RequestCancelled = 0x10c

// ClientConn manages HTTP/3 frame exchanges over a quic.Conn session.
type ClientConn struct {
	conn     *quic.Conn
	qpack    *QPACKCodec
	settings Settings

	closeOnce sync.Once
	closed    chan struct{}
}

// NewClientConn initializes an HTTP/3 client connection and opens control streams.
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
		_ = conn.CloseWithError(0x100, "failed control stream setup")
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
		str.CancelRead(0x103)
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
	for {
		frameType, payloadLen, err := ReadFrameHeader(r)
		if err != nil {
			return
		}

		if frameType == FrameTypeGoAway {
			cc.handleGoAway(r, payloadLen)
			return
		}

		if _, err := io.CopyN(io.Discard, r, int64(payloadLen)); err != nil {
			return
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
	var headerBuf bytes.Buffer
	if err := cc.qpack.EncodeRequestHeaders(&headerBuf, req, headerOrder); err != nil {
		return err
	}

	headerBlock := headerBuf.Bytes()

	var frameHead []byte
	frameHead = appendHeadersHeader(frameHead, uint64(len(headerBlock)))

	if _, err := str.Write(frameHead); err != nil {
		return err
	}

	if _, err := str.Write(headerBlock); err != nil {
		return err
	}

	body := req.Body()
	if len(body) > 0 {
		var dataHead []byte
		dataHead = appendDataHeader(dataHead, uint64(len(body)))

		if _, err := str.Write(dataHead); err != nil {
			return err
		}

		if _, err := str.Write(body); err != nil {
			return err
		}
	}

	return nil
}

func (cc *ClientConn) readResponse(str *quic.Stream, resp *fasthttp.Response) (trailers map[string][]string, err error) {
	r := quicvarint.NewReader(str)
	headersParsed := false

	for {
		frameType, payloadLen, err := ReadFrameHeader(r)
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		switch frameType {
		case FrameTypeHeaders:
			headerBlock := make([]byte, payloadLen)
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

			lr := io.LimitReader(r, int64(payloadLen))
			buf := make([]byte, min(payloadLen, 32768))

			for {
				n, rErr := lr.Read(buf)
				if n > 0 {
					resp.AppendBody(buf[:n])
				}

				if rErr == io.EOF {
					break
				}

				if rErr != nil {
					return nil, rErr
				}
			}

		default:
			if _, err := io.CopyN(io.Discard, r, int64(payloadLen)); err != nil {
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
