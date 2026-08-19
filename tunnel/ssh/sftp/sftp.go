// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sftp provides high-performance SFTP and SCP file transfer services over SSH sessions.
package sftp

import (
	"context"
	"fmt"
	"os"
	"sync"

	pkgsftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// NewClient initializes a new SFTP client over the active SSH connection using asynchronous write/read pipelining.
func NewClient(sshClient *ssh.Client, maxPacketSize uint32, opts ...pkgsftp.ClientOption) (*pkgsftp.Client, error) {
	if sshClient == nil {
		return nil, ErrClientClosed
	}

	maxPacket := int(maxPacketSize)
	if maxPacket <= 0 {
		maxPacket = 64 * 1024
	}

	opts = append([]pkgsftp.ClientOption{
		pkgsftp.MaxPacketUnchecked(maxPacket),
		pkgsftp.UseConcurrentWrites(true),
		pkgsftp.UseConcurrentReads(true),
	}, opts...)

	return pkgsftp.NewClient(sshClient, opts...)
}

// Upload transfers a local file to remotePath over SFTP using pooled buffers.
func Upload(sshClient *ssh.Client, localPath, remotePath string, maxPacketSize uint32) error {
	localFile, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer localFile.Close()

	sftpClient, err := NewClient(sshClient, maxPacketSize)
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()

	bufPtr := ioBufferPool.Get().(*[]byte)
	_, err = remoteFile.ReadFrom(localFile)

	ioBufferPool.Put(bufPtr)

	return err
}

// Download transfers a remote file to localPath over SFTP using pooled buffers and flushes disk writes.
func Download(sshClient *ssh.Client, remotePath, localPath string, maxPacketSize uint32) error {
	localFile, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer localFile.Close()

	sftpClient, err := NewClient(sshClient, maxPacketSize)
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	remoteFile, err := sftpClient.Open(remotePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()

	_, err = remoteFile.WriteTo(localFile)
	if err != nil {
		return err
	}

	return localFile.Sync()
}

// UploadParallel uploads a local file to remotePath over SFTP using multiple parallel worker threads writing at calculated offsets.
func UploadParallel(
	ctx context.Context,
	sshClient *ssh.Client,
	localPath, remotePath string,
	workers int,
	chunkSize int64,
	maxPacketSize uint32,
) error {
	if workers <= 0 {
		workers = 4
	}

	if chunkSize <= 0 {
		return ErrInvalidChunkSize
	}

	localFile, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer localFile.Close()

	info, err := localFile.Stat()
	if err != nil {
		return err
	}

	sftpClient, err := NewClient(sshClient, maxPacketSize)
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()

	totalSize := info.Size()
	if totalSize == 0 {
		return nil
	}

	return dispatchParallelUpload(ctx, localFile, remoteFile, totalSize, chunkSize, workers)
}

// DownloadParallel downloads a remote file to localPath over SFTP using multiple parallel worker threads reading at calculated offsets.
func DownloadParallel(
	ctx context.Context,
	sshClient *ssh.Client,
	remotePath, localPath string,
	workers int,
	chunkSize int64,
	maxPacketSize uint32,
) error {
	if workers <= 0 {
		workers = 4
	}

	if chunkSize <= 0 {
		return ErrInvalidChunkSize
	}

	sftpClient, err := NewClient(sshClient, maxPacketSize)
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	remoteFile, err := sftpClient.Open(remotePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()

	stat, err := remoteFile.Stat()
	if err != nil {
		return err
	}

	localFile, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer localFile.Close()

	totalSize := stat.Size()
	if totalSize == 0 {
		return localFile.Sync()
	}

	if err := dispatchParallelDownload(ctx, remoteFile, localFile, totalSize, chunkSize, workers); err != nil {
		return err
	}

	return localFile.Sync()
}

type transferTask struct {
	offset int64
	size   int64
}

func dispatchParallelUpload(
	ctx context.Context,
	localFile *os.File,
	remoteFile *pkgsftp.File,
	totalSize, chunkSize int64,
	workers int,
) error {
	taskCh := make(chan transferTask, workers*2)
	errCh := make(chan error, workers)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for task := range taskCh {
				if err := uploadChunk(localFile, remoteFile, task.offset, task.size); err != nil {
					select {
					case errCh <- err:
					default:
					}

					cancel()

					return
				}
			}
		})
	}

	go produceChunks(ctx, taskCh, totalSize, chunkSize)

	wg.Wait()
	close(errCh)

	if err, hasErr := <-errCh; hasErr {
		return fmt.Errorf("%w: %w", ErrParallelTransferFailed, err)
	}

	return nil
}

func dispatchParallelDownload(
	ctx context.Context,
	remoteFile *pkgsftp.File,
	localFile *os.File,
	totalSize, chunkSize int64,
	workers int,
) error {
	taskCh := make(chan transferTask, workers*2)
	errCh := make(chan error, workers)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for task := range taskCh {
				if err := downloadChunk(remoteFile, localFile, task.offset, task.size); err != nil {
					select {
					case errCh <- err:
					default:
					}

					cancel()

					return
				}
			}
		})
	}

	go produceChunks(ctx, taskCh, totalSize, chunkSize)

	wg.Wait()
	close(errCh)

	if err, hasErr := <-errCh; hasErr {
		return fmt.Errorf("%w: %w", ErrParallelTransferFailed, err)
	}

	return nil
}

func produceChunks(ctx context.Context, taskCh chan<- transferTask, totalSize, chunkSize int64) {
	defer close(taskCh)

	for offset := int64(0); offset < totalSize; offset += chunkSize {
		size := min(chunkSize, totalSize-offset)
		select {
		case <-ctx.Done():
			return
		case taskCh <- transferTask{offset: offset, size: size}:
		}
	}
}

func uploadChunk(localFile *os.File, remoteFile *pkgsftp.File, offset, size int64) error {
	bufPtr := ioBufferPool.Get().(*[]byte)
	defer ioBufferPool.Put(bufPtr)

	buf := *bufPtr

	var written int64

	for written < size {
		readLen := min(int64(len(buf)), size-written)

		n, err := localFile.ReadAt(buf[:readLen], offset+written)
		if n > 0 {
			if _, wErr := remoteFile.WriteAt(buf[:n], offset+written); wErr != nil {
				return wErr
			}

			written += int64(n)
		}

		if err != nil && written < size {
			return err
		}
	}

	return nil
}

func downloadChunk(remoteFile *pkgsftp.File, localFile *os.File, offset, size int64) error {
	bufPtr := ioBufferPool.Get().(*[]byte)
	defer ioBufferPool.Put(bufPtr)

	buf := *bufPtr

	var written int64

	for written < size {
		readLen := min(int64(len(buf)), size-written)

		n, err := remoteFile.ReadAt(buf[:readLen], offset+written)
		if n > 0 {
			if _, wErr := localFile.WriteAt(buf[:n], offset+written); wErr != nil {
				return wErr
			}

			written += int64(n)
		}

		if err != nil && written < size {
			return err
		}
	}

	return nil
}
