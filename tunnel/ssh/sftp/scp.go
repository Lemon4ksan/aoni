// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sftp

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

var ioBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 64*1024)
		return &b
	},
}

// WriteFile streams size bytes from r into remoteFilePath on the target server using native SCP protocol and zero-alloc buffer pooling.
func WriteFile(ctx context.Context, sshClient *ssh.Client, r io.Reader, size int64, remoteFilePath string) error {
	if sshClient == nil {
		return ErrClientClosed
	}

	fileName := filepath.Base(remoteFilePath)
	if containsControlChars(remoteFilePath) || containsControlChars(fileName) {
		return ErrInvalidTargetFile
	}

	sess, err := sshClient.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	w, err := sess.StdinPipe()
	if err != nil {
		return err
	}

	copyErrCh := make(chan error, 1)
	go func() {
		defer w.Close()

		var hdrStack [128]byte

		hdrBuf := append(hdrStack[:0], "C0644 "...)
		hdrBuf = strconv.AppendInt(hdrBuf, size, 10)
		hdrBuf = append(hdrBuf, ' ')
		hdrBuf = append(hdrBuf, fileName...)
		hdrBuf = append(hdrBuf, '\n')

		if _, err := w.Write(hdrBuf); err != nil {
			copyErrCh <- err
			return
		}

		if size > 0 {
			bufPtr := ioBufferPool.Get().(*[]byte)
			_, copyErr := io.CopyBuffer(w, r, *bufPtr)
			ioBufferPool.Put(bufPtr)

			if copyErr != nil {
				copyErrCh <- copyErr
				return
			}
		}

		var termStack [1]byte

		termStack[0] = 0x00

		_, err := w.Write(termStack[:])
		copyErrCh <- err
	}()

	execErr := sess.Start("scp -tr " + shellQuote(remoteFilePath))
	if execErr != nil {
		return execErr
	}

	copyErr := <-copyErrCh
	if copyErr != nil {
		return copyErr
	}

	return sess.Wait()
}

// Scp uploads localFilePath to remoteFilePath on the remote machine using native SCP protocol with zero-alloc buffer pooling.
func Scp(ctx context.Context, sshClient *ssh.Client, localFilePath, remoteFilePath string) error {
	file, err := os.Open(localFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	return WriteFile(ctx, sshClient, file, info.Size(), remoteFilePath)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func containsControlChars(s string) bool {
	return strings.ContainsAny(s, "\x00\n\r")
}
