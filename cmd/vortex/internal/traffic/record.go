// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package traffic

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/lemon4ksan/aoni/internal/compress/brotli"
)

// CmdRecord captures live HTTP/HTTPS traffic from applications into standard W3C HAR 1.2 files.
type CmdRecord struct{}

func (c *CmdRecord) Name() string      { return "record" }
func (c *CmdRecord) Aliases() []string { return []string{"sniff", "capture"} }
func (c *CmdRecord) Synopsis() string {
	return "Capture live HTTP/HTTPS network traffic into W3C HAR 1.2 specification"
}

func (c *CmdRecord) Usage() string {
	return "vortex traffic record [-port=9090] [-out=traffic.har] [-- <command> [args...]]"
}

type harEntry struct {
	StartedDateTime string  `json:"startedDateTime"`
	Time            int64   `json:"time"`
	Request         harReq  `json:"request"`
	Response        harResp `json:"response"`
}

type harReq struct {
	Method      string   `json:"method"`
	URL         string   `json:"url"`
	Headers     []harNV  `json:"headers"`
	QueryString []harNV  `json:"queryString"`
	PostData    *harPost `json:"postData,omitempty"`
}

type harResp struct {
	Status  int         `json:"status"`
	Headers []harNV     `json:"headers"`
	Content *harContent `json:"content,omitempty"`
}

type harNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPost struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Encoding string `json:"encoding,omitempty"`
}

type harContent struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Encoding string `json:"encoding,omitempty"`
}

type harLogContainer struct {
	Log struct {
		Version string     `json:"version"`
		Creator harCreator `json:"creator"`
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type sessionRecorder struct {
	mu      sync.Mutex
	entries []harEntry
	outFile string
}

func (sr *sessionRecorder) record(e harEntry) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	sr.entries = append(sr.entries, e)
}

func (sr *sessionRecorder) save() error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	var container harLogContainer

	container.Log.Version = "1.2"
	container.Log.Creator = harCreator{Name: "Vortex Traffic Recorder", Version: "1.0"}
	container.Log.Entries = sr.entries

	data, err := json.MarshalIndent(container, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(sr.outFile)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}

	return os.WriteFile(sr.outFile, data, 0o600)
}

// mitmCA dynamically generates TLS certificates for HTTPS MITM inspection.
type mitmCA struct {
	caCert *x509.Certificate
	caKey  crypto.PrivateKey
	caPEM  []byte
	mu     sync.RWMutex
	cache  map[string]*tls.Certificate
}

func newMitmCA() (*mitmCA, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating CA private key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating CA serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Vortex Traffic Recorder Dynamic CA"},
			CommonName:   "Vortex Ephemeral Root CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            2,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("creating CA certificate: %w", err)
	}

	caCert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing CA certificate: %w", err)
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	return &mitmCA{
		caCert: caCert,
		caKey:  priv,
		caPEM:  caPEM,
		cache:  make(map[string]*tls.Certificate),
	}, nil
}

func (m *mitmCA) getCertificate(rawHost string) (*tls.Certificate, error) {
	host := rawHost
	if h, _, err := net.SplitHostPort(rawHost); err == nil {
		host = h
	}

	m.mu.RLock()

	if cert, ok := m.cache[host]; ok {
		m.mu.RUnlock()
		return cert, nil
	}

	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if cert, ok := m.cache[host]; ok {
		return cert, nil
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating leaf key for %s: %w", host, err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating leaf serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   host,
			Organization: []string{"Vortex Traffic Recorder"},
		},
		NotBefore:   time.Now().Add(-1 * time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, m.caCert, &priv.PublicKey, m.caKey)
	if err != nil {
		return nil, fmt.Errorf("signing leaf certificate for %s: %w", host, err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshaling leaf private key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("creating TLS keypair for %s: %w", host, err)
	}

	m.cache[host] = &tlsCert

	return &tlsCert, nil
}

func (c *CmdRecord) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		portFlag   = fs.Int("port", 9090, "Local proxy port to listen on")
		outFlag    = fs.String("out", "traffic.har", "Output W3C HAR file path")
		targetFlag = fs.String("target", "", "Optional upstream target URL for reverse proxy mode")
		execFlag   = fs.String("exec", "", "Execute specified command string directly")
		waitFlag   = fs.Bool(
			"wait",
			false,
			"Keep proxy listening until Enter or Ctrl+C is pressed (ideal for GUI launchers)",
		)
		quietFlag = fs.Bool(
			"q",
			false,
			"Quiet mode: suppress live transaction logs (recommended for interactive TUIs)",
		)
		quietLong   = fs.Bool("quiet", false, "Alias for -q")
		silentFlag  = fs.Bool("silent", false, "Alias for -q")
		verboseFlag = fs.Bool("v", false, "Verbose mode: print live request lines during subprocess run")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex traffic record — Live Network Traffic Recorder & Process Runner\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex traffic record [flags] [-- <command> [args...]]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(stderr, "  vortex traffic record -out=app.har -- ./mycli      # Process capture\n")
		fmt.Fprintf(stderr, "  vortex traffic record -out=api.har -v -- python    # Verbose capture\n")
		fmt.Fprintf(stderr, "  vortex traffic record -port=9090 -out=traffic.har  # Standing proxy\n")
	}

	var (
		flagArgs []string
		cmdToRun []string
	)

	for i, arg := range args {
		if arg == "--" {
			flagArgs = args[:i]
			cmdToRun = args[i+1:]
			break
		}
	}

	if cmdToRun == nil {
		flagArgs = args
	}

	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	if len(cmdToRun) == 0 {
		if *execFlag != "" {
			cmdToRun = strings.Fields(*execFlag)
		} else if len(fs.Args()) > 0 {
			cmdToRun = fs.Args()
		}
	}

	isQuiet := *quietFlag || *quietLong || *silentFlag
	if len(cmdToRun) > 0 && !*verboseFlag {
		isQuiet = true
	}

	var logWriter io.Writer
	if !isQuiet {
		if len(cmdToRun) > 0 {
			logWriter = stderr
		} else {
			logWriter = stdout
		}
	}

	ca, err := newMitmCA()
	if err != nil {
		return fmt.Errorf("initializing ephemeral TLS MITM CA: %w", err)
	}

	caFilePath := filepath.Join(os.TempDir(), "vortex_ca.pem")
	if err := os.WriteFile(caFilePath, ca.caPEM, 0o600); err != nil {
		return fmt.Errorf("writing dynamic CA file: %w", err)
	}

	recorder := &sessionRecorder{
		entries: make([]harEntry, 0),
		outFile: *outFlag,
	}

	var (
		upstreamURL *url.URL
		revProxy    *httputil.ReverseProxy
	)

	if *targetFlag != "" {
		var err error

		upstreamURL, err = url.Parse(*targetFlag)
		if err != nil {
			return fmt.Errorf("parsing upstream target URL: %w", err)
		}

		revProxy = httputil.NewSingleHostReverseProxy(upstreamURL)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle HTTPS CONNECT tunnel with TLS MITM Decryption
		if r.Method == http.MethodConnect {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijacking not supported", http.StatusInternalServerError)
				return
			}

			clientConn, _, hErr := hijacker.Hijack()
			if hErr != nil {
				return
			}

			// Acknowledge CONNECT tunnel establishment
			_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

			// Generate on-the-fly certificate for the target host
			leafCert, cErr := ca.getCertificate(r.Host)
			if cErr != nil {
				_ = clientConn.Close()
				return
			}

			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{*leafCert},
				MinVersion:   tls.VersionTLS12,
				NextProtos:   []string{"http/1.1"},
			}

			tlsConn := tls.Server(clientConn, tlsConfig)

			go handleDecryptedHTTPS(tlsConn, r.Host, recorder, logWriter)

			return
		}

		start := time.Now()

		var reqBodyBytes []byte
		if r.Body != nil {
			reqBodyBytes, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewBuffer(reqBodyBytes))
		}

		var reqHeaders []harNV
		for k, v := range r.Header {
			for _, val := range v {
				reqHeaders = append(reqHeaders, harNV{Name: k, Value: val})
			}
		}

		var reqQueries []harNV
		for k, v := range r.URL.Query() {
			for _, val := range v {
				reqQueries = append(reqQueries, harNV{Name: k, Value: val})
			}
		}

		rec := &recordResponseWriter{ResponseWriter: w, body: &bytes.Buffer{}}

		targetURI := r.URL.String()
		switch {
		case revProxy != nil:
			r.Host = upstreamURL.Host
			revProxy.ServeHTTP(rec, r)
			targetURI = upstreamURL.String() + r.URL.RequestURI()
		case r.URL.IsAbs():
			// Forward proxy to absolute URL
			fwdReq, err := http.NewRequestWithContext( //nolint:gosec
				r.Context(),
				r.Method,
				r.URL.String(),
				bytes.NewBuffer(reqBodyBytes),
			)
			if err == nil {
				fwdReq.Header = r.Header.Clone()

				fwdResp, fErr := http.DefaultTransport.RoundTrip(fwdReq)
				if fErr == nil {
					defer fwdResp.Body.Close()

					for k, v := range fwdResp.Header {
						for _, val := range v {
							w.Header().Add(k, val)
							rec.Header().Add(k, val)
						}
					}

					rec.status = fwdResp.StatusCode
					w.WriteHeader(fwdResp.StatusCode)
					_, _ = io.Copy(rec, fwdResp.Body)
				}
			}

		default:
			// Fallback local echo if no upstream specified
			rec.Header().Set("Content-Type", "application/json")
			rec.WriteHeader(http.StatusOK)
			_, _ = rec.Write([]byte(`{"status":"ok"}`))
		}

		duration := time.Since(start).Milliseconds()

		var postData *harPost
		if len(reqBodyBytes) > 0 {
			postData = &harPost{
				MimeType: r.Header.Get("Content-Type"),
				Text:     string(reqBodyBytes),
			}
		}

		var respContent *harContent
		if rec.body.Len() > 0 {
			respContent = &harContent{
				Size:     int64(rec.body.Len()),
				MimeType: rec.Header().Get("Content-Type"),
				Text:     rec.body.String(),
			}
		}

		var respHeaders []harNV
		for k, v := range rec.Header() {
			for _, val := range v {
				respHeaders = append(respHeaders, harNV{Name: k, Value: val})
			}
		}

		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}

		entry := harEntry{
			StartedDateTime: start.Format(time.RFC3339),
			Time:            duration,
			Request: harReq{
				Method:      r.Method,
				URL:         targetURI,
				Headers:     reqHeaders,
				QueryString: reqQueries,
				PostData:    postData,
			},
			Response: harResp{
				Status:  status,
				Headers: respHeaders,
				Content: respContent,
			},
		}

		recorder.record(entry)
		_ = recorder.save()

		if logWriter != nil {
			fmt.Fprintf(logWriter, "✔ [%s] %s %s -> HTTP %d (%d ms, %d bytes)\n",
				start.Format("15:04:05"), r.Method, r.URL.Path, status, duration, rec.body.Len())
		}
	})

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", *portFlag))
	if err != nil {
		return fmt.Errorf("binding recorder proxy port %d: %w", *portFlag, err)
	}

	actualPort := ln.Addr().(*net.TCPAddr).Port

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErrChan := make(chan error, 1)
	go func() {
		if sErr := server.Serve(ln); sErr != nil && sErr != http.ErrServerClosed {
			serverErrChan <- sErr
		}
	}()

	// MODE A: Process execution wrapper mode
	if len(cmdToRun) > 0 {
		proxyURL := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

		if !isQuiet {
			fmt.Fprintf(stderr, "⚡ Vortex Process Traffic Sniffer active (proxy: %s)\n", proxyURL)
			fmt.Fprintf(stderr, "⚡ Spawning isolated subprocess: %s\n", strings.Join(cmdToRun, " "))
			fmt.Fprintf(stderr, "⚡ Recording output to: %s\n", *outFlag)
			fmt.Fprintln(stderr, "────────────────────────────────────────────────────────────────────────")
		}

		cmdArgs := cmdToRun[1:]

		ext := strings.ToLower(filepath.Ext(cmdToRun[0]))
		if ext == ".exe" || ext == "" {
			cmdArgs = append(cmdArgs,
				"--proxy-server="+proxyURL,
				"--ignore-certificate-errors",
			)
		}

		subCmd := exec.CommandContext(ctx, cmdToRun[0], cmdArgs...) //nolint:gosec
		subCmd.Stdin = os.Stdin
		subCmd.Stdout = stdout
		subCmd.Stderr = stderr

		// Inject process-isolated proxy and root CA environment variables
		subCmd.Env = append(os.Environ(),
			"HTTP_PROXY="+proxyURL,
			"HTTPS_PROXY="+proxyURL,
			"http_proxy="+proxyURL,
			"https_proxy="+proxyURL,
			"ALL_PROXY="+proxyURL,
			"all_proxy="+proxyURL,
			"SSL_CERT_FILE="+caFilePath,
			"CURL_CA_BUNDLE="+caFilePath,
			"REQUESTS_CA_BUNDLE="+caFilePath,
			"NODE_EXTRA_CA_CERTS="+caFilePath,
			"NODE_TLS_REJECT_UNAUTHORIZED=0",
			"GIT_SSL_CAINFO="+caFilePath,
			"PYTHONHTTPSVERIFY=0",
		)

		cleanupCA := installSystemRootCA(caFilePath)
		defer cleanupCA()

		runStart := time.Now()
		runErr := subCmd.Run()
		runDuration := time.Since(runStart)

		// If the process exited quickly (launcher/updater) or -wait flag is set, keep the proxy open until user confirms
		if *waitFlag || (runDuration < 4*time.Second && len(recorder.entries) == 0) {
			fmt.Fprintf(
				stdout,
				"\n⚡ Launcher process completed in %v (background app is likely running).\n",
				runDuration.Round(time.Millisecond),
			)
			fmt.Fprintf(stdout, "⚡ Recorder proxy is ACTIVE on %s (isolated to this process tree)\n", proxyURL)
			fmt.Fprintf(
				stdout,
				"👉 Use your application normally. When finished, press [ENTER] here to save %s...\n",
				*outFlag,
			)

			waitChan := make(chan struct{})
			go func() {
				reader := bufio.NewReader(os.Stdin)
				_, _ = reader.ReadString('\n')

				close(waitChan)
			}()

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

			select {
			case <-waitChan:
			case <-sigChan:
			case <-ctx.Done():
			}
		}

		// Brief flush window for trailing keep-alive requests
		time.Sleep(200 * time.Millisecond)

		_ = server.Shutdown(ctx)
		_ = recorder.save()

		if !isQuiet {
			fmt.Fprintln(stderr, "────────────────────────────────────────────────────────────────────────")
		}

		fmt.Fprintf(
			stdout,
			"\n✔ Subprocess finished. Captured %d transaction(s) to %s\n\n",
			len(recorder.entries),
			*outFlag,
		)
		fmt.Fprintf(stdout, "👉 Next: Synthesize Go contract & Mock Server:\n")
		fmt.Fprintf(stdout, "   vortex init -from-har=\"%s\" -pkg=api -service=API -out=api.go\n", *outFlag)
		fmt.Fprintf(stdout, "   vortex gen api.go\n")
		fmt.Fprintf(stdout, "   vortex mock api.go\n")

		if runErr != nil {
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				return fmt.Errorf("subprocess exited with error: %w", runErr)
			}

			return runErr
		}

		return nil
	}

	// MODE B: Standing background proxy server mode
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Fprintf(stdout, "⚡ Vortex Traffic Recorder active on http://127.0.0.1:%d\n", actualPort)
	fmt.Fprintf(stdout, "⚡ Capturing live transactions to %s (Press Ctrl+C to stop)\n\n", *outFlag)
	fmt.Fprintf(stdout, "Usage in another terminal:\n")
	fmt.Fprintf(
		stdout,
		"  $env:HTTP_PROXY=\"http://127.0.0.1:%d\"; $env:HTTPS_PROXY=\"http://127.0.0.1:%d\"\n",
		actualPort,
		actualPort,
	)
	fmt.Fprintf(stdout, "  $env:SSL_CERT_FILE=\"%s\"; $env:NODE_EXTRA_CA_CERTS=\"%s\"\n", caFilePath, caFilePath)
	fmt.Fprintf(stdout, "  ./mycli run\n\n")

	select {
	case sErr := <-serverErrChan:
		return fmt.Errorf("proxy listener error: %w", sErr)
	case <-sigChan:
		fmt.Fprintf(stdout, "\n⚡ Stopping traffic recorder and flushing %s...\n", *outFlag)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = server.Shutdown(shutdownCtx)
	_ = recorder.save()

	fmt.Fprintf(stdout, "✔ Saved %d transaction(s) to %s\n\n", len(recorder.entries), *outFlag)
	fmt.Fprintf(stdout, "👉 Next step: Generate Go client contract and Mock Server:\n")
	fmt.Fprintf(stdout, "   vortex init -from-har=\"%s\" -pkg=api -service=API -out=api.go\n", *outFlag)
	fmt.Fprintf(stdout, "   vortex gen api.go\n")
	fmt.Fprintf(stdout, "   vortex mock api.go\n")

	return nil
}

func handleDecryptedHTTPS(
	tlsConn net.Conn,
	targetHost string,
	recorder *sessionRecorder,
	logWriter io.Writer,
) {
	defer tlsConn.Close()

	reader := bufio.NewReader(tlsConn)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
		Timeout: 60 * time.Second,
	}

	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}

		start := time.Now()

		var reqBodyBytes []byte
		if req.Body != nil {
			reqBodyBytes, _ = io.ReadAll(req.Body)
			_ = req.Body.Close()
		}

		var reqHeaders []harNV
		for k, v := range req.Header {
			for _, val := range v {
				reqHeaders = append(reqHeaders, harNV{Name: k, Value: val})
			}
		}

		var reqQueries []harNV
		for k, v := range req.URL.Query() {
			for _, val := range v {
				reqQueries = append(reqQueries, harNV{Name: k, Value: val})
			}
		}

		targetURI := fmt.Sprintf("https://%s%s", targetHost, req.URL.RequestURI())

		fwdReq, err := http.NewRequestWithContext( //nolint:gosec
			req.Context(),
			req.Method,
			targetURI,
			bytes.NewBuffer(reqBodyBytes),
		)
		if err != nil {
			_ = (&http.Response{
				StatusCode: http.StatusBadGateway,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Body:       io.NopCloser(strings.NewReader(err.Error())),
			}).Write(tlsConn)

			return
		}

		fwdReq.Header = req.Header.Clone()
		fwdReq.Header.Del("Proxy-Connection")

		fwdResp, fErr := client.Do(fwdReq) //nolint:gosec
		if fErr != nil {
			_ = (&http.Response{
				StatusCode: http.StatusBadGateway,
				ProtoMajor: 1,
				ProtoMinor: 1,
				Body:       io.NopCloser(strings.NewReader(fErr.Error())),
			}).Write(tlsConn)

			return
		}

		// Check for WebSocket upgrade
		if fwdResp.StatusCode == http.StatusSwitchingProtocols ||
			strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			_ = fwdResp.Write(tlsConn)
			if serverConn, ok := fwdResp.Body.(io.ReadWriteCloser); ok {
				go func() {
					_, _ = io.Copy(serverConn, tlsConn)
					_ = serverConn.Close()
				}()

				_, _ = io.Copy(tlsConn, serverConn)
			}

			return
		}

		isStreaming := strings.Contains(fwdResp.Header.Get("Content-Type"), "text/event-stream") ||
			strings.Contains(fwdResp.Header.Get("Content-Type"), "ndjson") ||
			strings.Contains(fwdResp.Header.Get("Content-Type"), "grpc") ||
			(fwdResp.ContentLength < 0 && len(fwdResp.TransferEncoding) > 0)

		var respBodyBytes []byte
		if isStreaming {
			var captured bytes.Buffer

			tee := io.TeeReader(fwdResp.Body, io.MultiWriter(tlsConn, &captured))
			fwdResp.Body = io.NopCloser(tee)
			_ = fwdResp.Write(tlsConn)
			_ = fwdResp.Body.Close()
			respBodyBytes = captured.Bytes()
		} else {
			respBodyBytes, _ = io.ReadAll(fwdResp.Body)
			_ = fwdResp.Body.Close()

			fwdResp.Body = io.NopCloser(bytes.NewBuffer(respBodyBytes))
			fwdResp.ContentLength = int64(len(respBodyBytes))

			if err := fwdResp.Write(tlsConn); err != nil {
				return
			}
		}

		var respHeaders []harNV
		for k, v := range fwdResp.Header {
			for _, val := range v {
				respHeaders = append(respHeaders, harNV{Name: k, Value: val})
			}
		}

		duration := time.Since(start).Milliseconds()

		var postData *harPost
		if len(reqBodyBytes) > 0 {
			postData = &harPost{
				MimeType: req.Header.Get("Content-Type"),
				Text:     string(reqBodyBytes),
			}
		}

		var respContent *harContent
		if len(respBodyBytes) > 0 {
			bodyText, encoding, decompressed := decodePayloadForHAR(
				respBodyBytes,
				fwdResp.Header.Get("Content-Encoding"),
			)
			if decompressed {
				var filtered []harNV
				for _, h := range respHeaders {
					if !strings.EqualFold(h.Name, "Content-Encoding") {
						filtered = append(filtered, h)
					}
				}

				respHeaders = filtered
			}

			respContent = &harContent{
				Size:     int64(len(bodyText)),
				MimeType: fwdResp.Header.Get("Content-Type"),
				Text:     bodyText,
				Encoding: encoding,
			}
		}

		entry := harEntry{
			StartedDateTime: start.Format(time.RFC3339),
			Time:            duration,
			Request: harReq{
				Method:      req.Method,
				URL:         targetURI,
				Headers:     reqHeaders,
				QueryString: reqQueries,
				PostData:    postData,
			},
			Response: harResp{
				Status:  fwdResp.StatusCode,
				Headers: respHeaders,
				Content: respContent,
			},
		}

		recorder.record(entry)
		_ = recorder.save()

		if logWriter != nil {
			fmt.Fprintf(logWriter, "✔ [%s] %s %s -> HTTP %d (%d ms, %d bytes)\n",
				start.Format("15:04:05"), req.Method, req.URL.Path, fwdResp.StatusCode, duration, len(respBodyBytes))
		}

		if req.Close || fwdResp.Close {
			return
		}
	}
}

type recordResponseWriter struct {
	http.ResponseWriter
	status int
	body   *bytes.Buffer
}

func (r *recordResponseWriter) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recordResponseWriter) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func installSystemRootCA(caFilePath string) func() {
	if runtime.GOOS == "windows" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Temporary add to current user Root store
		addCmd := exec.CommandContext(ctx, "certutil", "-addstore", "-user", "Root", caFilePath)
		_ = addCmd.Run()

		return func() {
			cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cleanCancel()

			delCmd := exec.CommandContext(cleanCtx, "certutil", "-delstore", "-user", "Root", "Vortex MITM Root CA")
			_ = delCmd.Run()
		}
	}

	return func() {}
}

func decodePayloadForHAR(data []byte, contentEncoding string) (string, string, bool) {
	if len(data) == 0 {
		return "", "", false
	}

	enc := strings.ToLower(strings.TrimSpace(contentEncoding))
	decompressed := false

	switch {
	case enc == "gzip" || (len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b):
		if gzReader, err := gzip.NewReader(bytes.NewReader(data)); err == nil {
			if dec, err := io.ReadAll(gzReader); err == nil {
				_ = gzReader.Close()
				data = dec
				decompressed = true
			}
		}

	case enc == "br":
		brReader := brotli.NewReader(bytes.NewReader(data))
		if dec, err := io.ReadAll(brReader); err == nil {
			data = dec
			decompressed = true
		}

	case enc == "deflate":
		if zr, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
			if dec, err := io.ReadAll(zr); err == nil {
				_ = zr.Close()
				data = dec
				decompressed = true
			}
		}
	}

	if utf8.Valid(data) {
		return string(data), "", decompressed
	}

	return base64.StdEncoding.EncodeToString(data), "base64", decompressed
}
