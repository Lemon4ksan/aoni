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
	case StreamTypeQPACKEncoder, StreamTypeQPACKDecoder:
		return
	default:
		str.CancelRead(0x103)
	}
}

func (cc *ClientConn) readControlStream(r quicvarint.Reader) {
	for {
		frameType, payloadLen, err := ReadFrameHeader(r)
		if err != nil {
			return
		}

		if frameType == FrameTypeGoAway {
			_ = cc.Close()
			return
		}

		if _, err := io.CopyN(io.Discard, r, int64(payloadLen)); err != nil {
			return
		}
	}
}

// Do executes a fasthttp.Request over a QUIC stream and populates fasthttp.Response.
func (cc *ClientConn) Do(ctx context.Context, req *fasthttp.Request, resp *fasthttp.Response, headerOrder []string) error {
	str, err := cc.conn.OpenStreamSync(ctx)
	if err != nil {
		return err
	}

	defer str.Close()

	if err := cc.sendRequest(str, req, headerOrder); err != nil {
		str.CancelWrite(0x10c)
		return err
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

func (cc *ClientConn) readResponse(str *quic.Stream, resp *fasthttp.Response) error {
	r := quicvarint.NewReader(str)
	headersParsed := false

	for {
		frameType, payloadLen, err := ReadFrameHeader(r)
		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		switch frameType {
		case FrameTypeHeaders:
			if headersParsed {
				return ErrFrameUnexpected
			}

			headerBlock := make([]byte, payloadLen)

			if _, err := io.ReadFull(r, headerBlock); err != nil {
				return err
			}

			if err := cc.qpack.DecodeResponseHeaders(headerBlock, &resp.Header); err != nil {
				return err
			}

			headersParsed = true

		case FrameTypeData:
			if !headersParsed {
				return ErrFrameUnexpected
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
					return rErr
				}
			}

		default:
			if _, err := io.CopyN(io.Discard, r, int64(payloadLen)); err != nil {
				return err
			}
		}
	}

	return nil
}

// Close gracefully terminates the HTTP/3 client connection.
func (cc *ClientConn) Close() error {
	cc.closeOnce.Do(func() {
		close(cc.closed)
		_ = cc.conn.CloseWithError(0x100, "connection closed")
	})

	return nil
}
