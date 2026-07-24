// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valyala/fasthttp"
)

const maxConsecutiveControlFrames = 1000

// FrameWithHeaders defines an interface for HTTP/2 frames that carry raw HPACK-encoded header fragments.
type FrameWithHeaders interface {
	Headers() []byte
}

// ConnOpts defines connection configuration options.
type ConnOpts struct {
	PingInterval        time.Duration
	DisablePingChecking bool
	OnDisconnect        func(c *Conn)
}

// Conn manages a multiplexed HTTP/2 connection over a net.Conn socket.
type Conn struct {
	c                  net.Conn
	br                 *bufio.Reader
	bw                 *bufio.Writer
	enc                *HPACK
	dec                *HPACK
	nextID             uint32
	serverWindow       int32
	serverStreamWindow int32
	maxWindow          int32
	currentWindow      int32
	openStreams        int32
	current            Settings
	serverS            Settings
	reqQueued          sync.Map
	in                 chan *Context
	out                chan *FrameHeader
	pingInterval       time.Duration
	unacks             int
	disableAcks        bool
	lastErr            error
	onDisconnect       func(*Conn)
	closed             uint64
	orderedKeys        []string

	windowMu   sync.Mutex
	windowCond *sync.Cond
}

// NewConn instantiates a new Conn wrapping socket c.
func NewConn(c net.Conn, opts ConnOpts) *Conn {
	nc := &Conn{
		c:             c,
		br:            bufio.NewReaderSize(c, 4096),
		bw:            bufio.NewWriterSize(c, int(maxFrameSize)),
		enc:           AcquireHPACK(),
		dec:           AcquireHPACK(),
		nextID:        1,
		maxWindow:     1 << 20,
		currentWindow: 1 << 20,
		in:            make(chan *Context, 128),
		out:           make(chan *FrameHeader, 128),
		pingInterval:  opts.PingInterval,
		disableAcks:   opts.DisablePingChecking,
		onDisconnect:  opts.OnDisconnect,
	}

	nc.windowCond = sync.NewCond(&nc.windowMu)
	nc.current.SetMaxWindowSize(1 << 20)
	nc.current.SetPush(false)

	return nc
}

// SetOrderedHeaders configures custom HPACK header sequence for anti-detect fingerprinting.
func (c *Conn) SetOrderedHeaders(keys []string) {
	c.orderedKeys = keys
}

// CancelStream terminates an active HTTP/2 stream by transmitting an RST_STREAM frame.
func (c *Conn) CancelStream(ctx *Context) {
	if ctx == nil || ctx.StreamID == 0 {
		return
	}

	if ctx.State() == streamClosed {
		return
	}

	ctx.SetState(streamClosed)
	c.reqQueued.Delete(ctx.StreamID)
	atomic.AddInt32(&c.openStreams, -1)

	fr := AcquireFrameHeader()
	fr.SetStream(ctx.StreamID)

	rst := AcquireFrame(FrameResetStream).(*RstStream)
	rst.SetCode(StreamCanceled)
	fr.SetBody(rst)

	select {
	case c.out <- fr:
	default:
		ReleaseFrameHeader(fr)
	}

	c.broadcastWindowUpdate()
}

// Handshake performs HTTP/2 connection initialization.
func (c *Conn) Handshake() error {
	if err := PerformHandshake(true, c.bw, &c.current, c.maxWindow-65535); err != nil {
		_ = c.c.Close()
		return err
	}

	fr, err := ReadFrameFrom(c.br)
	if err != nil {
		_ = c.c.Close()
		return err
	}

	if fr.Type() != FrameSettings {
		_ = c.c.Close()
		ReleaseFrameHeader(fr)

		return fmt.Errorf("aoni h2engine: expected SETTINGS frame, got %s", fr.Type())
	}

	st := fr.Body().(*Settings)
	if !st.IsAck() {
		st.CopyTo(&c.serverS)
		c.serverStreamWindow += int32(c.serverS.MaxWindowSize())

		if st.HeaderTableSize() <= defaultHeaderTableSize {
			c.enc.SetMaxTableSize(st.HeaderTableSize())
		}

		c.sendSettingsAck()
	}

	ReleaseFrameHeader(fr)

	go c.writeLoop()
	go c.readLoop()

	return nil
}

func (c *Conn) sendSettingsAck() {
	fr := AcquireFrameHeader()
	stRes := AcquireFrame(FrameSettings).(*Settings)
	stRes.SetAck(true)
	fr.SetBody(stRes)

	if _, err := fr.WriteTo(c.bw); err == nil {
		_ = c.bw.Flush()
	}

	ReleaseFrameHeader(fr)
}

// CanOpenStream reports whether the client can open new concurrent streams.
func (c *Conn) CanOpenStream() bool {
	return atomic.LoadInt32(&c.openStreams) < int32(c.serverS.maxStreams)
}

// Closed reports whether the connection has been closed.
func (c *Conn) Closed() bool {
	return atomic.LoadUint64(&c.closed) == 1
}

// Close gracefully terminates the HTTP/2 connection.
func (c *Conn) Close() error {
	if !atomic.CompareAndSwapUint64(&c.closed, 0, 1) {
		return io.EOF
	}

	close(c.in)

	fr := AcquireFrameHeader()
	defer ReleaseFrameHeader(fr)

	ga := AcquireFrame(FrameGoAway).(*GoAway)
	ga.SetStream(0)
	ga.SetCode(NoError)
	fr.SetBody(ga)

	_, err := fr.WriteTo(c.bw)
	if err == nil {
		err = c.bw.Flush()
	}

	_ = c.c.Close()
	c.broadcastWindowUpdate()

	if c.onDisconnect != nil {
		c.onDisconnect(c)
	}

	return err
}

// Write enqueues a request context for execution.
func (c *Conn) Write(r *Context) {
	c.in <- r
}

func (c *Conn) writeLoop() {
	var lastErr error

	defer func() { _ = c.Close() }()
	defer c.recoverWriteLoop(&lastErr)

	if c.pingInterval <= 0 {
		c.pingInterval = DefaultPingInterval
	}

	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()

	for {
		if stop, err := c.selectWriteEvent(ticker.C); stop {
			lastErr = err
			break
		}

		if !c.disableAcks && c.unacks >= 3 {
			lastErr = ErrTimeout
			break
		}
	}
}

func (c *Conn) selectWriteEvent(pingChan <-chan time.Time) (bool, error) {
	select {
	case ctx, ok := <-c.in:
		if !ok {
			return true, nil
		}

		if err := c.writeRequest(ctx); err != nil {
			ctx.Err <- err
			if errors.Is(err, ErrNotAvailableStreams) {
				return false, nil
			}

			return true, err
		}

	case fr, ok := <-c.out:
		if !ok {
			return true, nil
		}

		defer ReleaseFrameHeader(fr)

		if _, err := fr.WriteTo(c.bw); err != nil || c.bw.Flush() != nil {
			return true, err
		}

	case <-pingChan:
		if err := c.writePing(); err != nil {
			return true, err
		}
	}

	return false, nil
}

func (c *Conn) recoverWriteLoop(lastErr *error) {
	if r := recover(); r != nil && *lastErr == nil {
		if err, ok := r.(error); ok {
			*lastErr = err
		} else {
			*lastErr = fmt.Errorf("aoni h2engine panic: %v", r)
		}
	}

	if *lastErr == nil {
		*lastErr = io.ErrUnexpectedEOF
	}

	c.reqQueued.Range(func(_, v any) bool {
		if ctx, ok := v.(*Context); ok {
			ctx.Err <- *lastErr
		}

		return true
	})
}

func (c *Conn) finish(r *Context, stream uint32, err error) {
	atomic.AddInt32(&c.openStreams, -1)
	r.Err <- err
	c.reqQueued.Delete(stream)
	close(r.Err)
}

func (c *Conn) readLoop() {
	defer func() { _ = c.Close() }()

	for {
		fr, err := c.readNext()
		if err != nil {
			c.lastErr = err
			break
		}

		if ri, ok := c.reqQueued.Load(fr.Stream()); ok {
			r := ri.(*Context)
			if r.State() == streamClosed {
				ReleaseFrameHeader(fr)
				continue
			}

			err := c.readStream(fr, r)
			if err == nil {
				if fr.Flags().Has(FlagEndStream) {
					r.SetState(streamClosed)
					c.finish(r, fr.Stream(), nil)
				}
			} else {
				r.SetState(streamClosed)
				c.finish(r, fr.Stream(), err)

				if errors.Is(err, FlowControlError) {
					ReleaseFrameHeader(fr)
					break
				}
			}
		}

		ReleaseFrameHeader(fr)
	}
}

func isExpectContinue(req *fasthttp.Request) bool {
	expect := req.Header.Peek("Expect")
	return bytes.EqualFold(expect, []byte("100-continue"))
}

func (c *Conn) waitExpectContinue(ctx *Context) {
	t := time.NewTimer(1 * time.Second)
	defer t.Stop()

	select {
	case <-t.C:
		// ExpectContinueTimeout elapsed; proceed to transmit body payload
	case <-ctx.Err:
		// Early response arrived (100 Continue or 4xx/5xx error rejection)
	}
}

func (c *Conn) writeRequest(ctx *Context) error {
	if !c.CanOpenStream() {
		return ErrNotAvailableStreams
	}

	req := ctx.Request
	hasBody := len(req.Body()) != 0

	id := c.nextID
	c.nextID += 2
	ctx.StreamID = id
	ctx.SetState(streamOpen)

	initWin := int32(c.serverS.MaxWindowSize())
	if initWin <= 0 {
		initWin = 65535
	}

	atomic.StoreInt32(&ctx.streamWindow, initWin)

	fr := AcquireFrameHeader()
	defer ReleaseFrameHeader(fr)

	fr.SetStream(id)

	h := AcquireFrame(FrameHeaders).(*Headers)
	fr.SetBody(h)

	c.encodeRequestHeaders(h, req)

	h.SetPadding(false)
	h.SetEndStream(!hasBody)
	h.SetEndHeaders(true)

	if !hasBody {
		ctx.SetState(streamHalfClosed)
	}

	c.reqQueued.Store(id, ctx)

	_, err := fr.WriteTo(c.bw)
	if err != nil {
		c.lastErr = err
		c.reqQueued.Delete(id)
		ctx.SetState(streamClosed)

		return err
	}

	_ = c.bw.Flush()

	if hasBody {
		ReleaseFrame(h)

		if isExpectContinue(req) {
			c.waitExpectContinue(ctx)
		}

		if ctx.State() != streamClosed {
			err = c.writeData(fr, ctx, req.Body())
		}
	}

	if err == nil {
		atomic.AddInt32(&c.openStreams, 1)
	} else {
		c.lastErr = err
		c.reqQueued.Delete(id)
		ctx.SetState(streamClosed)
	}

	return err
}

func (c *Conn) writeData(fh *FrameHeader, ctx *Context, body []byte) error {
	data := AcquireFrame(FrameData).(*Data)
	fh.SetBody(data)

	offset := 0
	bodyLen := len(body)

	for offset < bodyLen {
		if c.Closed() || ctx.State() == streamClosed {
			return ErrStreamClosed
		}

		remaining := bodyLen - offset
		chunkSize := c.calculateChunkSize(ctx, remaining)

		if chunkSize <= 0 {
			c.waitForWindowUpdate(ctx, remaining)
			continue
		}

		end := offset + chunkSize
		data.SetEndStream(end == bodyLen)
		data.SetPadding(false)
		data.SetData(body[offset:end])

		if _, err := fh.WriteTo(c.bw); err != nil {
			return err
		}

		atomic.AddInt32(&c.serverWindow, -int32(chunkSize))
		atomic.AddInt32(&ctx.streamWindow, -int32(chunkSize))
		offset = end
	}

	if bodyLen > 0 {
		ctx.SetState(streamHalfClosed)
	}

	return nil
}

// waitForWindowUpdate blocks the writing goroutine until flow control window capacity expands
// or until the stream/connection terminates.
//
// Preconditions:
//   - Evaluates calculateChunkSize inside c.windowMu lock to prevent lost wake-up signal races.
func (c *Conn) waitForWindowUpdate(ctx *Context, remaining int) {
	c.windowMu.Lock()
	defer c.windowMu.Unlock()

	if c.Closed() || ctx.State() == streamClosed {
		return
	}

	if c.calculateChunkSize(ctx, remaining) > 0 {
		return
	}

	c.windowCond.Wait()
}

func (c *Conn) broadcastWindowUpdate() {
	c.windowMu.Lock()
	c.windowCond.Broadcast()
	c.windowMu.Unlock()
}

func (c *Conn) calculateChunkSize(ctx *Context, remaining int) int {
	maxFrame := int(c.serverS.MaxFrameSize())
	if maxFrame <= 0 {
		maxFrame = 16384
	}

	serverWin := atomic.LoadInt32(&c.serverWindow)
	streamWin := atomic.LoadInt32(&ctx.streamWindow)

	win := int(serverWin)
	if int(streamWin) < win {
		win = int(streamWin)
	}

	if win <= 0 {
		return 0
	}

	chunk := min(remaining, win)

	return min(chunk, maxFrame)
}

func (c *Conn) encodeRequestHeaders(h *Headers, req *fasthttp.Request) {
	hf := AcquireHeaderField()
	defer ReleaseHeaderField(hf)

	enc := c.enc

	hf.SetBytes(StringAuthority, req.URI().Host())
	enc.AppendHeaderField(h, hf, true)

	hf.SetBytes(StringMethod, req.Header.Method())
	enc.AppendHeaderField(h, hf, true)

	hf.SetBytes(StringPath, req.URI().RequestURI())
	enc.AppendHeaderField(h, hf, true)

	hf.SetBytes(StringScheme, req.URI().Scheme())
	enc.AppendHeaderField(h, hf, true)

	hf.SetBytes(StringUserAgent, req.Header.UserAgent())
	enc.AppendHeaderField(h, hf, true)

	if len(c.orderedKeys) > 0 {
		c.appendOrderedHeaders(h, req, hf)
	} else {
		req.Header.All()(func(k, v []byte) bool {
			if bytes.EqualFold(k, StringUserAgent) {
				return true
			}

			hf.SetBytes(toLowerCopy(k), v)
			enc.AppendHeaderField(h, hf, false)
			return true
		})
	}
}

func (c *Conn) appendOrderedHeaders(h *Headers, req *fasthttp.Request, hf *HeaderField) {
	visited := make(map[string]bool)

	for _, key := range c.orderedKeys {
		if key == "" || key[0] == ':' || bytes.EqualFold([]byte(key), StringUserAgent) {
			continue
		}

		val := req.Header.Peek(key)
		if len(val) > 0 {
			hf.SetBytes(toLowerCopy([]byte(key)), val)
			c.enc.AppendHeaderField(h, hf, false)
			visited[key] = true
		}
	}

	req.Header.All()(func(k, v []byte) bool {
		if visited[string(k)] || bytes.EqualFold(k, StringUserAgent) {
			return true
		}

		hf.SetBytes(toLowerCopy(k), v)
		c.enc.AppendHeaderField(h, hf, false)
		return true
	})
}

func (c *Conn) readNext() (*FrameHeader, error) {
	for {
		fr, err := ReadFrameFrom(c.br)
		if err != nil {
			return nil, err
		}

		if fr.Type() == FrameData || fr.Type() == FrameHeaders {
			c.unacks = 0
		} else {
			c.unacks++
			if c.unacks > maxConsecutiveControlFrames {
				ReleaseFrameHeader(fr)
				return nil, ErrControlFrameFlood
			}
		}

		if fr.Stream() != 0 {
			return fr, nil
		}

		if err := c.handleConnectionFrame(fr); err != nil {
			ReleaseFrameHeader(fr)
			return nil, err
		}

		ReleaseFrameHeader(fr)
	}
}

func (c *Conn) handleConnectionFrame(fr *FrameHeader) error {
	switch fr.Type() {
	case FrameSettings:
		st := fr.Body().(*Settings)
		if !st.IsAck() {
			c.handleSettings(st)
		}
	case FrameWindowUpdate:
		return c.handleWindowUpdate(fr)
	case FramePing:
		ping := fr.Body().(*Ping)
		if !ping.IsAck() {
			c.handlePing(ping)
		} else {
			c.unacks--
		}
	case FrameGoAway:
		ga := fr.Body().(*GoAway)
		c.handleGoAway(ga)
	}

	return nil
}

func (c *Conn) handleWindowUpdate(fr *FrameHeader) error {
	wu := fr.Body().(*WindowUpdate)
	inc := int32(wu.Increment())
	if inc <= 0 {
		return ErrInvalidWindowIncrement
	}

	streamID := fr.Stream()

	var err error
	if streamID == 0 {
		err = c.updateServerWindow(inc)
	} else {
		err = c.updateStreamWindow(streamID, inc)
	}

	if err == nil {
		c.broadcastWindowUpdate()
	}

	return err
}

func (c *Conn) updateServerWindow(inc int32) error {
	old := atomic.LoadInt32(&c.serverWindow)
	if old > 0 && old > (1<<31-1)-inc {
		return ErrWindowAboveLimits
	}

	atomic.AddInt32(&c.serverWindow, inc)

	return nil
}

func (c *Conn) updateStreamWindow(streamID uint32, inc int32) error {
	v, ok := c.reqQueued.Load(streamID)
	if !ok {
		return nil
	}

	reqCtx, ok := v.(*Context)
	if !ok {
		return nil
	}

	old := atomic.LoadInt32(&reqCtx.streamWindow)
	if old > 0 && old > (1<<31-1)-inc {
		return ErrWindowAboveLimits
	}

	atomic.AddInt32(&reqCtx.streamWindow, inc)

	return nil
}

func (c *Conn) handleGoAway(ga *GoAway) {
	lastStreamID := ga.Stream()

	c.reqQueued.Range(func(key, value any) bool {
		streamID, ok := key.(uint32)
		if !ok {
			return true
		}

		reqCtx, ok := value.(*Context)
		if !ok {
			return true
		}

		if streamID > lastStreamID {
			c.reqQueued.Delete(streamID)
			select {
			case reqCtx.Err <- ErrGoAwayRetryable:
			default:
			}
		}

		return true
	})

	_ = c.Close()
}

func (c *Conn) writePing() error {
	fr := AcquireFrameHeader()
	defer ReleaseFrameHeader(fr)

	ping := AcquireFrame(FramePing).(*Ping)
	ping.SetCurrentTime()
	fr.SetBody(ping)

	_, err := fr.WriteTo(c.bw)
	if err == nil {
		err = c.bw.Flush()
		if err == nil {
			c.unacks++
		}
	}

	return err
}

func (c *Conn) handleSettings(st *Settings) {
	st.CopyTo(&c.serverS)
	c.serverStreamWindow += int32(c.serverS.MaxWindowSize())
	c.enc.SetMaxTableSize(st.HeaderTableSize())

	fr := AcquireFrameHeader()
	stRes := AcquireFrame(FrameSettings).(*Settings)
	stRes.SetAck(true)
	fr.SetBody(stRes)

	c.out <- fr
}

func (c *Conn) handlePing(ping *Ping) {
	fr := AcquireFrameHeader()
	ping.SetAck(true)
	fr.SetBody(ping)

	c.out <- fr
}

func (c *Conn) readStream(fr *FrameHeader, reqCtx *Context) error {
	switch fr.Type() {
	case FrameHeaders, FrameContinuation:
		h := fr.Body().(FrameWithHeaders)
		if reqCtx.headersParsed {
			return c.readTrailers(h.Headers(), reqCtx)
		}

		statusCode, err := c.readHeader(h.Headers(), reqCtx.Response)
		if err == nil {
			if statusCode < 100 || statusCode >= 200 || statusCode == 101 {
				reqCtx.headersParsed = true
			}
		}

		return err

	case FrameData:
		c.currentWindow -= int32(fr.Len())
		currentWin := c.currentWindow
		c.serverWindow -= int32(fr.Len())

		data := fr.Body().(*Data)
		if data.Len() != 0 {
			reqCtx.Response.AppendBody(data.Data())
			c.updateWindow(fr.Stream(), fr.Len())
		}

		if currentWin < c.maxWindow/2 {
			nValue := c.maxWindow - currentWin
			c.currentWindow = c.maxWindow
			c.updateWindow(0, int(nValue))
		}
	}

	return nil
}

func (c *Conn) readTrailers(b []byte, reqCtx *Context) error {
	hf := AcquireHeaderField()
	defer ReleaseHeaderField(hf)

	var (
		totalSize uint32
		maxList   = c.current.MaxHeaderListSize()
	)

	if maxList == 0 {
		maxList = defaultMaxHeaderListSize
	}

	if reqCtx.Trailers == nil {
		reqCtx.Trailers = make(map[string][]string)
	}

	for len(b) > 0 {
		var err error
		b, err = c.dec.Next(hf, b)

		if err != nil {
			return err
		}

		totalSize += hf.Size()
		if totalSize > maxList {
			return ErrPayloadExceeds
		}

		if !hf.IsPseudo() && len(hf.KeyBytes()) > 0 {
			key := hf.Key()
			reqCtx.Trailers[key] = append(reqCtx.Trailers[key], hf.Value())
		}
	}

	return nil
}

func (c *Conn) updateWindow(streamID uint32, size int) {
	fr := AcquireFrameHeader()
	fr.SetStream(streamID)

	wu := AcquireFrame(FrameWindowUpdate).(*WindowUpdate)
	wu.SetIncrement(size)
	fr.SetBody(wu)

	c.out <- fr
}

const defaultMaxHeaderListSize = 10 * 1024 * 1024

func (c *Conn) readHeader(b []byte, res *fasthttp.Response) (int, error) {
	hf := AcquireHeaderField()
	defer ReleaseHeaderField(hf)

	var (
		err        error
		statusCode int
		totalSize  uint32
		maxList    = c.current.MaxHeaderListSize()
	)

	if maxList == 0 {
		maxList = defaultMaxHeaderListSize
	}

	for len(b) > 0 {
		b, err = c.dec.Next(hf, b)
		if err != nil {
			return 0, err
		}

		totalSize += hf.Size()
		if totalSize > maxList {
			return 0, ErrPayloadExceeds
		}

		if hf.IsPseudo() && len(hf.KeyBytes()) > 1 && hf.KeyBytes()[1] == 's' {
			n, pErr := strconv.ParseInt(hf.Value(), 10, 64)
			if pErr != nil {
				return 0, pErr
			}

			statusCode = int(n)
			if statusCode < 100 || statusCode >= 200 || statusCode == 101 {
				res.SetStatusCode(statusCode)
			}

			continue
		}

		if bytes.Equal(hf.KeyBytes(), StringContentLength) {
			n, _ := strconv.Atoi(hf.Value())
			res.Header.SetContentLength(n)
		} else {
			res.Header.AddBytesKV(hf.KeyBytes(), hf.ValueBytes())
		}
	}

	return statusCode, nil
}

// Dialer establishes HTTP/2 TLS connections using custom network dialers.
type Dialer struct {
	Addr         string
	TLSConfig    *tls.Config
	PingInterval time.Duration
	NetDial      fasthttp.DialFunc
	RawDial      fasthttp.DialFunc
}

// Dial creates and performs an HTTP/2 handshake on a new connection.
func (d *Dialer) Dial(opts ConnOpts) (*Conn, error) {
	c, err := d.tryDial()
	if err != nil {
		return nil, err
	}

	nc := NewConn(c, opts)
	err = nc.Handshake()

	return nc, err
}

func (d *Dialer) tryDial() (net.Conn, error) {
	if d.RawDial != nil {
		return d.RawDial(d.Addr)
	}

	if d.TLSConfig == nil {
		d.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS13,
		}
	}

	if d.TLSConfig.ServerName == "" {
		host, _, err := net.SplitHostPort(d.Addr)
		if err != nil {
			host = d.Addr
		}

		d.TLSConfig.ServerName = host
	}

	d.TLSConfig.NextProtos = append(d.TLSConfig.NextProtos, "h2")

	var (
		c   net.Conn
		err error
	)

	if d.NetDial != nil {
		c, err = d.NetDial(d.Addr)
	} else {
		c, err = net.Dial("tcp", d.Addr)
	}

	if err != nil {
		return nil, err
	}

	tlsConn := tls.Client(c, d.TLSConfig)
	if err := tlsConn.Handshake(); err != nil {
		_ = c.Close()
		return nil, err
	}

	if tlsConn.ConnectionState().NegotiatedProtocol != "h2" {
		_ = c.Close()
		return nil, ErrServerSupport
	}

	return tlsConn, nil
}
