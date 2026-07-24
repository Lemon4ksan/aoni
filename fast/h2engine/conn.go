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

	nc.current.SetMaxWindowSize(1 << 20)
	nc.current.SetPush(false)

	return nc
}

// SetOrderedHeaders configures custom HPACK header sequence for anti-detect fingerprinting.
func (c *Conn) SetOrderedHeaders(keys []string) {
	c.orderedKeys = keys
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
			err := c.readStream(fr, r.Response)

			if err == nil {
				if fr.Flags().Has(FlagEndStream) {
					c.finish(r, fr.Stream(), nil)
				}
			} else {
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

func (c *Conn) writeRequest(ctx *Context) error {
	if !c.CanOpenStream() {
		return ErrNotAvailableStreams
	}

	req := ctx.Request
	hasBody := len(req.Body()) != 0

	id := c.nextID
	c.nextID += 2

	fr := AcquireFrameHeader()
	defer ReleaseFrameHeader(fr)

	fr.SetStream(id)

	h := AcquireFrame(FrameHeaders).(*Headers)
	fr.SetBody(h)

	c.encodeRequestHeaders(h, req)

	h.SetPadding(false)
	h.SetEndStream(!hasBody)
	h.SetEndHeaders(true)

	c.reqQueued.Store(id, ctx)

	_, err := fr.WriteTo(c.bw)
	if err == nil && hasBody {
		ReleaseFrame(h)
		err = writeData(c.bw, fr, req.Body())
	}

	if err == nil {
		err = c.bw.Flush()
		if err == nil {
			atomic.AddInt32(&c.openStreams, 1)
		}
	}

	if err != nil {
		c.lastErr = err
		c.reqQueued.Delete(id)
	}

	return err
}

func (c *Conn) encodeRequestHeaders(h *Headers, req *fasthttp.Request) {
	hf := AcquireHeaderField()
	defer ReleaseHeaderField(hf)

	enc := c.enc

	// 1. Pseudo-headers
	hf.SetBytes(StringAuthority, req.URI().Host())
	enc.AppendHeaderField(h, hf, true)

	hf.SetBytes(StringMethod, req.Header.Method())
	enc.AppendHeaderField(h, hf, true)

	hf.SetBytes(StringPath, req.URI().RequestURI())
	enc.AppendHeaderField(h, hf, true)

	hf.SetBytes(StringScheme, req.URI().Scheme())
	enc.AppendHeaderField(h, hf, true)

	// 2. User-Agent
	hf.SetBytes(StringUserAgent, req.Header.UserAgent())
	enc.AppendHeaderField(h, hf, true)

	// 3. Regular Headers (with ordered keys support if provided)
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

func writeData(bw *bufio.Writer, fh *FrameHeader, body []byte) error {
	step := 1 << 14

	data := AcquireFrame(FrameData).(*Data)
	fh.SetBody(data)

	var err error

	for i := 0; err == nil && i < len(body); i += step {
		if i+step >= len(body) {
			step = len(body) - i
		}

		data.SetEndStream(i+step == len(body))
		data.SetPadding(false)
		data.SetData(body[i : step+i])

		_, err = fh.WriteTo(bw)
	}

	return err
}

func (c *Conn) readNext() (*FrameHeader, error) {
	for {
		fr, err := ReadFrameFrom(c.br)
		if err != nil {
			return nil, err
		}

		if fr.Stream() != 0 {
			return fr, nil
		}

		c.handleConnectionFrame(fr)
		ReleaseFrameHeader(fr)
	}
}

func (c *Conn) handleConnectionFrame(fr *FrameHeader) {
	switch fr.Type() {
	case FrameSettings:
		st := fr.Body().(*Settings)
		if !st.IsAck() {
			c.handleSettings(st)
		}
	case FrameWindowUpdate:
		win := int32(fr.Body().(*WindowUpdate).Increment())
		atomic.AddInt32(&c.serverWindow, win)
	case FramePing:
		ping := fr.Body().(*Ping)
		if !ping.IsAck() {
			c.handlePing(ping)
		} else {
			c.unacks--
		}
	case FrameGoAway:
		_ = c.Close()
	}
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

func (c *Conn) readStream(fr *FrameHeader, res *fasthttp.Response) error {
	switch fr.Type() {
	case FrameHeaders, FrameContinuation:
		h := fr.Body().(FrameWithHeaders)
		return c.readHeader(h.Headers(), res)

	case FrameData:
		c.currentWindow -= int32(fr.Len())
		currentWin := c.currentWindow
		c.serverWindow -= int32(fr.Len())

		data := fr.Body().(*Data)
		if data.Len() != 0 {
			res.AppendBody(data.Data())
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

func (c *Conn) updateWindow(streamID uint32, size int) {
	fr := AcquireFrameHeader()
	fr.SetStream(streamID)

	wu := AcquireFrame(FrameWindowUpdate).(*WindowUpdate)
	wu.SetIncrement(size)
	fr.SetBody(wu)

	c.out <- fr
}

func (c *Conn) readHeader(b []byte, res *fasthttp.Response) error {
	hf := AcquireHeaderField()
	defer ReleaseHeaderField(hf)

	var err error

	for len(b) > 0 {
		b, err = c.dec.Next(hf, b)
		if err != nil {
			return err
		}

		if hf.IsPseudo() && len(hf.KeyBytes()) > 1 && hf.KeyBytes()[1] == 's' {
			n, pErr := strconv.ParseInt(hf.Value(), 10, 64)
			if pErr != nil {
				return pErr
			}

			res.SetStatusCode(int(n))

			continue
		}

		if bytes.Equal(hf.KeyBytes(), StringContentLength) {
			n, _ := strconv.Atoi(hf.Value())
			res.Header.SetContentLength(n)
		} else {
			res.Header.AddBytesKV(hf.KeyBytes(), hf.ValueBytes())
		}
	}

	return nil
}

// Dialer establishes HTTP/2 TLS connections using custom network dialers.
type Dialer struct {
	Addr         string
	TLSConfig    *tls.Config
	PingInterval time.Duration
	NetDial      fasthttp.DialFunc // Dial func returning raw TCP conn requiring TLS handshake
	RawDial      fasthttp.DialFunc // Dial func returning pre-handshaked TLS conn (e.g. uTLS)
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
