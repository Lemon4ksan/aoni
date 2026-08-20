// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package client provides an enterprise-grade SSH tunneling, command execution, and file transfer client.
package client

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	pkgsftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"

	"github.com/lemon4ksan/aoni/tunnel/ssh/sftp"
)

const (
	// DefaultWindowSize specifies the default initial SSH channel window size (16MB).
	DefaultWindowSize uint32 = 16 * 1024 * 1024

	// DefaultMaxPacketSize specifies the default maximum SSH packet size (64KB).
	DefaultMaxPacketSize uint32 = 64 * 1024
)

var (
	ioBufferPool = generic.NewPool(func() *[]byte {
		b := make([]byte, 64*1024)
		return &b
	})

	bufioReaderPool = generic.NewPool(func() *bufio.Reader {
		return bufio.NewReaderSize(nil, 64*1024)
	})
)

// Client represents an SSH client session routed through aoni's L4 network transport pipeline.
type Client struct {
	*ssh.Client

	User          string
	Addr          string
	Port          uint
	ProxyURL      string
	Jump          *Client
	Dialer        ContextDialer
	RequestPty    bool
	PtyTerm       string
	PtyWidth      int
	PtyHeight     int
	WindowSize    uint32
	MaxPacketSize uint32

	sftpMu     sync.Mutex
	sftpClient *pkgsftp.Client

	cancel context.CancelFunc
	closed atomic.Bool
}

// New establishes an SSH connection wrapped in aoni's transport pipeline.
func New(ctx context.Context, user, targetAddr string, opts ...Option) (*Client, error) {
	client := &Client{
		User:          user,
		Addr:          targetAddr,
		Port:          22,
		WindowSize:    DefaultWindowSize,
		MaxPacketSize: DefaultMaxPacketSize,
	}

	config := &ssh.ClientConfig{
		Config: ssh.Config{
			Ciphers: []string{
				"aes128-gcm@openssh.com",
				"chacha20-poly1305@openssh.com",
				"aes256-gcm@openssh.com",
			},
			KeyExchanges: []string{
				"curve25519-sha256",
				"curve25519-sha256@libssh.org",
				"ecdh-sha2-nistp256",
			},
		},
		User:          user,
		Timeout:       20 * time.Second,
		ClientVersion: "SSH-2.0-Aoni",
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}

		if err := opt(client, config); err != nil {
			return nil, err
		}
	}

	if err := Dial(ctx, client, config); err != nil {
		return nil, err
	}

	return client, nil
}

// Dial connects client to the remote target host using configured options, proxies, or jump hosts.
func Dial(ctx context.Context, c *Client, config *ssh.ClientConfig) error {
	if c.Jump != nil && c.ProxyURL != "" {
		return ErrProxyAndJumpConflict
	}

	if config.User == "" {
		config.User = resolveDefaultUsername(c.User)
		c.User = config.User
	}

	if config.HostKeyCallback == nil {
		callback, err := DefaultKnownHosts()
		if err != nil {
			return err
		}

		config.HostKeyCallback = callback
	}

	target := formatTargetAddr(c.Addr, c.Port)

	conn, err := dialTransportConn(ctx, c, target, config.Timeout)
	if err != nil {
		return err
	}

	optimizeTCPConn(conn)

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, target, config)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("%w: %w", ErrSSHDialFailed, err)
	}

	c.Client = ssh.NewClient(sshConn, chans, reqs)

	keepAliveCtx, cancel := context.WithCancel(context.Background())

	c.cancel = cancel
	go c.keepAliveLoop(keepAliveCtx, 15*time.Second)

	return nil
}

func formatTargetAddr(addr string, port uint) string {
	if port == 22 || port == 0 {
		if strings.IndexByte(addr, ':') != -1 {
			return addr
		}

		return addr + ":22"
	}

	return net.JoinHostPort(addr, strconv.FormatUint(uint64(port), 10))
}

func optimizeTCPConn(conn net.Conn) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}
}

// Run executes command on the remote host and returns its combined stdout and stderr.
func (c *Client) Run(ctx context.Context, cmdStr string) ([]byte, error) {
	cmd, err := c.Command(ctx, "/bin/sh", "-c", cmdStr)
	if err != nil {
		return nil, err
	}

	return cmd.CombinedOutput()
}

// Command instantiates a new Cmd targeting name with optional arguments.
func (c *Client) Command(ctx context.Context, name string, args ...string) (*Cmd, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if c.closed.Load() || c.Client == nil {
		return nil, ErrSSHClosed
	}

	sess, err := c.NewSession()
	if err != nil {
		return nil, err
	}

	if c.RequestPty {
		term := c.PtyTerm
		if term == "" {
			term = "xterm"
		}

		width := c.PtyWidth
		if width <= 0 {
			width = 80
		}

		height := c.PtyHeight
		if height <= 0 {
			height = 40
		}

		modes := ssh.TerminalModes{
			ssh.ECHO:          0,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}

		if err := sess.RequestPty(term, width, height, modes); err != nil {
			_ = sess.Close()
			return nil, fmt.Errorf("%w: %w", ErrPtyRequestFailed, err)
		}
	}

	cmdStr := name
	if len(args) > 0 {
		cmdStr = name + " " + strings.Join(args, " ")
	}

	return &Cmd{
		Session: sess,
		Path:    name,
		Args:    args,
		cmdStr:  cmdStr,
		ctx:     ctx,
	}, nil
}

// Stream executes command and streams stdout and stderr line-by-line over channels in real-time.
func (c *Client) Stream(
	ctx context.Context,
	command string,
	timeout time.Duration,
) (<-chan string, <-chan string, <-chan bool, <-chan error, error) {
	outCh := make(chan string, 100)
	errCh := make(chan string, 100)
	doneCh := make(chan bool, 1)
	cmdErrCh := make(chan error, 1)

	sess, err := c.NewSession()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	outPipe, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, nil, nil, nil, err
	}

	errPipe, err := sess.StderrPipe()
	if err != nil {
		_ = sess.Close()
		return nil, nil, nil, nil, err
	}

	if err := sess.Start(command); err != nil {
		_ = sess.Close()
		return nil, nil, nil, nil, err
	}

	execTimeout := timeout
	if execTimeout <= 0 {
		execTimeout = 60 * time.Second
	}

	go c.runStreamWorkers(ctx, sess, outPipe, errPipe, outCh, errCh, doneCh, cmdErrCh, execTimeout)

	return outCh, errCh, doneCh, cmdErrCh, nil
}

func (c *Client) runStreamWorkers(
	ctx context.Context,
	sess *ssh.Session,
	outPipe, errPipe io.Reader,
	outCh, errCh chan<- string,
	doneCh chan<- bool,
	cmdErrCh chan<- error,
	timeout time.Duration,
) {
	defer close(doneCh)
	defer close(cmdErrCh)
	defer func() { _ = sess.Close() }()

	streamCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var wg sync.WaitGroup

	wg.Go(func() { scanLines(streamCtx, outPipe, outCh) })
	wg.Go(func() { scanLines(streamCtx, errPipe, errCh) })

	resCh := make(chan error, 1)
	go func() {
		wg.Wait()

		resCh <- sess.Wait()
	}()

	select {
	case err := <-resCh:
		cmdErrCh <- err

		doneCh <- true
	case <-streamCtx.Done():
		_ = sess.Signal(ssh.SIGINT)

		cmdErrCh <- streamCtx.Err()

		doneCh <- false
	}
}

func scanLines(ctx context.Context, r io.Reader, ch chan<- string) {
	defer close(ch)

	reader := bufioReaderPool.Get()
	if reader == nil {
		reader = bufio.NewReaderSize(r, 64*1024)
	} else {
		reader.Reset(r)
	}

	defer func() {
		reader.Reset(nil)
		bufioReaderPool.Put(reader)
	}()

	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			cleanLine := strings.TrimRight(line, "\r\n")
			select {
			case ch <- cleanLine:
			case <-ctx.Done():
				return
			}
		}

		if err != nil {
			return
		}
	}
}

// NewSftp initializes a new SFTP client over the active SSH session.
func (c *Client) NewSftp(opts ...pkgsftp.ClientOption) (*pkgsftp.Client, error) {
	if c.closed.Load() || c.Client == nil {
		return nil, ErrSSHClosed
	}

	return sftp.NewClient(c.Client, c.MaxPacketSize, opts...)
}

// GetSftp returns a thread-safe, cached SFTP client instance, initializing it lazily on first access.
func (c *Client) GetSftp(opts ...pkgsftp.ClientOption) (*pkgsftp.Client, error) {
	if c.closed.Load() || c.Client == nil {
		return nil, ErrSSHClosed
	}

	c.sftpMu.Lock()
	defer c.sftpMu.Unlock()

	if c.sftpClient != nil {
		return c.sftpClient, nil
	}

	sftpClient, err := sftp.NewClient(c.Client, c.MaxPacketSize, opts...)
	if err != nil {
		return nil, err
	}

	c.sftpClient = sftpClient

	return c.sftpClient, nil
}

// Upload transfers a local file to remotePath on the remote server via SFTP.
func (c *Client) Upload(localPath, remotePath string) error {
	if c.closed.Load() || c.Client == nil {
		return ErrSSHClosed
	}

	sftpClient, err := c.GetSftp()
	if err != nil {
		return err
	}

	localFile, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer localFile.Close()

	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()

	bufPtr := ioBufferPool.Get()
	if bufPtr == nil {
		b := make([]byte, 64*1024)
		bufPtr = &b
	}

	_, err = io.CopyBuffer(remoteFile, localFile, *bufPtr)
	ioBufferPool.Put(bufPtr)

	return err
}

// Download transfers a remote file to localPath via SFTP.
func (c *Client) Download(remotePath, localPath string) error {
	if c.closed.Load() || c.Client == nil {
		return ErrSSHClosed
	}

	sftpClient, err := c.GetSftp()
	if err != nil {
		return err
	}

	localFile, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer localFile.Close()

	remoteFile, err := sftpClient.Open(remotePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()

	bufPtr := ioBufferPool.Get()
	if bufPtr == nil {
		b := make([]byte, 64*1024)
		bufPtr = &b
	}

	_, err = io.CopyBuffer(localFile, remoteFile, *bufPtr)
	ioBufferPool.Put(bufPtr)

	if err != nil {
		return err
	}

	return localFile.Sync()
}

// UploadParallel uploads a local file to remotePath over SFTP using multiple worker threads.
func (c *Client) UploadParallel(ctx context.Context, localPath, remotePath string, workers int, chunkSize int64) error {
	if c.closed.Load() || c.Client == nil {
		return ErrSSHClosed
	}

	return sftp.UploadParallel(ctx, c.Client, localPath, remotePath, workers, chunkSize, c.MaxPacketSize)
}

// DownloadParallel downloads a remote file to localPath over SFTP using multiple worker threads.
func (c *Client) DownloadParallel(
	ctx context.Context,
	remotePath, localPath string,
	workers int,
	chunkSize int64,
) error {
	if c.closed.Load() || c.Client == nil {
		return ErrSSHClosed
	}

	return sftp.DownloadParallel(ctx, c.Client, remotePath, localPath, workers, chunkSize, c.MaxPacketSize)
}

// WriteFile streams size bytes from r into remoteFilePath using SCP protocol.
func (c *Client) WriteFile(ctx context.Context, r io.Reader, size int64, remoteFilePath string) error {
	if c.closed.Load() || c.Client == nil {
		return ErrSSHClosed
	}

	return sftp.WriteFile(ctx, c.Client, r, size, remoteFilePath)
}

// Scp uploads localFilePath to remoteFilePath using SCP protocol.
func (c *Client) Scp(ctx context.Context, localFilePath, remoteFilePath string) error {
	if c.closed.Load() || c.Client == nil {
		return ErrSSHClosed
	}

	return sftp.Scp(ctx, c.Client, localFilePath, remoteFilePath)
}

// Script executes a script from r on the remote host using an interpreter.
func (c *Client) Script(ctx context.Context, r io.Reader, opts ...CmdOption) (*Cmd, error) {
	cmd, err := c.Command(ctx, "/bin/sh")
	if err != nil {
		return nil, err
	}

	for _, opt := range opts {
		if opt != nil {
			opt(cmd)
		}
	}

	cmd.Stdin = r

	return cmd, nil
}

// ScriptFile reads localPath into memory and executes it as a script on the remote host.
func (c *Client) ScriptFile(ctx context.Context, localPath string, opts ...CmdOption) (*Cmd, error) {
	scriptBytes, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("aoni ssh client: read script file: %w", err)
	}

	return c.Script(ctx, bytes.NewReader(scriptBytes), opts...)
}

// DialContext establishes a routed TCP connection through the remote SSH tunnel.
func (c *Client) DialContext(ctx context.Context, network, targetAddr string) (net.Conn, error) {
	if c.closed.Load() || c.Client == nil {
		return nil, ErrSSHClosed
	}

	type result struct {
		conn net.Conn
		err  error
	}

	resCh := make(chan result, 1)
	go func() {
		conn, err := c.Dial(network, targetAddr)
		resCh <- result{conn: conn, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		return res.conn, res.err
	}
}

// Close terminates the keep-alive worker, active SFTP client, and underlying SSH connection.
func (c *Client) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		if c.cancel != nil {
			c.cancel()
		}

		c.sftpMu.Lock()
		if c.sftpClient != nil {
			_ = c.sftpClient.Close()
			c.sftpClient = nil
		}

		c.sftpMu.Unlock()

		if c.Client != nil {
			return c.Client.Close()
		}
	}

	return nil
}

func (c *Client) keepAliveLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.closed.Load() {
				return
			}

			if _, _, err := c.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				_ = c.Close()
				return
			}
		}
	}
}

// Cmd represents an SSH command runner with context cancellation, signal control, and environment injection.
type Cmd struct {
	*ssh.Session

	Path   string
	Args   []string
	Env    []string
	cmdStr string

	Cancel func() error

	ctx      context.Context
	initOnce atomic.Bool
}

// CombinedOutput runs the command remotely and returns its combined stdout and stderr.
func (c *Cmd) CombinedOutput() ([]byte, error) {
	return c.runInContext(func() ([]byte, error) {
		return c.Session.CombinedOutput(c.String())
	})
}

// Output runs the command remotely and returns its stdout.
func (c *Cmd) Output() ([]byte, error) {
	return c.runInContext(func() ([]byte, error) {
		return c.Session.Output(c.String())
	})
}

// Run executes the command remotely and waits for completion.
func (c *Cmd) Run() error {
	_, err := c.runInContext(func() ([]byte, error) {
		return nil, c.Session.Run(c.String())
	})

	return err
}

// Start begins command execution on the remote host without waiting for completion.
func (c *Cmd) Start() error {
	_, err := c.runInContext(func() ([]byte, error) {
		return nil, c.Session.Start(c.String())
	})

	return err
}

// String formats the command line path and arguments without shell escaping.
func (c *Cmd) String() string {
	if c.cmdStr != "" {
		return c.cmdStr
	}

	if len(c.Args) == 0 {
		return c.Path
	}

	return c.Path + " " + strings.Join(c.Args, " ")
}

func (c *Cmd) init() error {
	if c.initOnce.Load() {
		return nil
	}

	for _, envVar := range c.Env {
		key, val, ok := strings.Cut(envVar, "=")
		if !ok {
			continue
		}

		if err := c.Setenv(key, val); err != nil {
			return fmt.Errorf("%w: setenv %s: %w", ErrCommandInitFailed, key, err)
		}
	}

	c.initOnce.Store(true)

	return nil
}

func (c *Cmd) runInContext(fn func() ([]byte, error)) ([]byte, error) {
	if err := c.init(); err != nil {
		return nil, err
	}

	if c.ctx == nil || c.ctx == context.Background() || c.ctx == context.TODO() || c.ctx.Done() == nil {
		return fn()
	}

	type result struct {
		out []byte
		err error
	}

	resCh := make(chan result, 1)
	go func() {
		out, err := fn()
		resCh <- result{out: out, err: err}
	}()

	select {
	case <-c.ctx.Done():
		c.terminate()
		return nil, c.ctx.Err()
	case res := <-resCh:
		return res.out, res.err
	}
}

func (c *Cmd) terminate() {
	if c.Cancel != nil {
		_ = c.Cancel()
		return
	}

	_ = c.Signal(ssh.SIGINT)
}

func resolveDefaultUsername(current string) string {
	if current != "" {
		return current
	}

	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}

	return "root"
}

// ContextDialer is a generic dialer capable of establishing network connections with a context.
type ContextDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

func dialTransportConn(ctx context.Context, c *Client, target string, timeout time.Duration) (net.Conn, error) {
	if c.Jump != nil {
		return c.Jump.DialContext(ctx, "tcp", target)
	}

	if c.Dialer != nil {
		return c.Dialer.DialContext(ctx, "tcp", target)
	}

	if c.ProxyURL != "" {
		return dialSocks5Proxy(ctx, c.ProxyURL, target, timeout)
	}

	dialer := &net.Dialer{Timeout: timeout}

	return dialer.DialContext(ctx, "tcp", target)
}

func dialSocks5Proxy(ctx context.Context, proxyURL, target string, timeout time.Duration) (net.Conn, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("aoni ssh client: invalid proxy url: %w", err)
	}

	proxyAddr := u.Host
	if proxyAddr == "" {
		proxyAddr = proxyURL
	}

	var auth *proxy.Auth
	if u.User != nil {
		auth = &proxy.Auth{User: u.User.Username()}
		if pass, ok := u.User.Password(); ok {
			auth.Password = pass
		}
	}

	baseDialer := &net.Dialer{Timeout: timeout}

	socksDialer, err := proxy.SOCKS5("tcp", proxyAddr, auth, baseDialer)
	if err != nil {
		return nil, fmt.Errorf("aoni ssh client: socks5 dialer setup: %w", err)
	}

	if cd, ok := socksDialer.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, "tcp", target)
	}

	return socksDialer.Dial("tcp", target)
}
