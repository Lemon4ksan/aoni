// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package tun

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	tunsetiff = 0x400454ca // TUNSETIFF ioctl command on Linux
	iffTun    = 0x0001     // IFF_TUN (Layer 3 IP packets, no Ethernet header)
	iffNoPI   = 0x1000     // IFF_NO_PI (Do not prepend packet information header)
)

var (
	// ErrLinuxTunOpenFailed indicates that /dev/net/tun could not be opened.
	ErrLinuxTunOpenFailed = errors.New("aoni tun: failed to open /dev/net/tun")

	// ErrLinuxIoctlFailed indicates that TUNSETIFF ioctl registration failed.
	ErrLinuxIoctlFailed = errors.New("aoni tun: TUNSETIFF ioctl failed")
)

type ifreq struct {
	Name  [16]byte
	Flags uint16
	_     [22]byte
}

// LinuxAdapter encapsulates a Linux /dev/net/tun virtual network interface.
type LinuxAdapter struct {
	file *os.File
	name string
}

// NewLinuxAdapter creates and registers a Layer 3 TUN network interface on Linux.
//
// Preconditions:
//   - Requires CAP_NET_ADMIN privileges on Linux (or running as root).
func NewLinuxAdapter(devName string) (*LinuxAdapter, error) {
	file, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLinuxTunOpenFailed, err)
	}

	var ifr ifreq

	ifr.Flags = iffTun | iffNoPI

	if devName != "" {
		copy(ifr.Name[:15], devName)
	}

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		file.Fd(),
		uintptr(tunsetiff),
		uintptr(unsafe.Pointer(&ifr)),
	)

	if errno != 0 {
		_ = file.Close()
		return nil, fmt.Errorf("%w: %w", ErrLinuxIoctlFailed, errno)
	}

	realName := cStringToGoString(ifr.Name[:])

	return &LinuxAdapter{
		file: file,
		name: realName,
	}, nil
}

func cStringToGoString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}

	return string(b)
}

// Name returns the actual interface name assigned by the Linux kernel (e.g. "tun0").
func (a *LinuxAdapter) Name() string {
	return a.name
}

// Read reads one Layer 3 IP packet from the Linux kernel virtual network interface.
func (a *LinuxAdapter) Read(b []byte) (int, error) {
	return a.file.Read(b)
}

// Write transmits an IP packet back into the Linux kernel network stack.
func (a *LinuxAdapter) Write(b []byte) (int, error) {
	return a.file.Write(b)
}

// Close releases the file descriptor and destroys the Linux TUN interface.
func (a *LinuxAdapter) Close() error {
	return a.file.Close()
}
