// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netdial

import (
	"context"
	"errors"
	"io"
	"net"
	"time"
)

// ErrL2DeviceNil is returned when attempting to bind an uninitialized L2Device.
var ErrL2DeviceNil = errors.New("aoni/netdial: L2Device cannot be nil")

// L2Addr represents a Data Link Layer (MAC) hardware network address satisfying net.Addr.
type L2Addr struct {
	HardwareAddr net.HardwareAddr
}

// Network returns the protocol name ("ethernet").
func (a *L2Addr) Network() string {
	return "ethernet"
}

// String returns the string representation of the MAC address.
func (a *L2Addr) String() string {
	if a == nil || a.HardwareAddr == nil {
		return "00:00:00:00:00:00"
	}

	return a.HardwareAddr.String()
}

var _ net.Addr = (*L2Addr)(nil)

// L2Device defines the raw Data Link Layer (Ethernet) frame I/O contract.
type L2Device interface {
	// WriteFrame transmits a raw Ethernet frame (including L2 header) to the network interface.
	WriteFrame(frame []byte) (n int, err error)

	// ReadFrame reads an incoming raw Ethernet frame from the network interface.
	ReadFrame(buf []byte) (n int, err error)

	// HardwareAddr returns the local MAC address of the device.
	HardwareAddr() net.HardwareAddr

	// MTU returns the Maximum Transmission Unit for L2 frames (typically 1500).
	MTU() int

	io.Closer
}

// RawStackDriver defines a custom L3/L4 network stack driver interface.
type RawStackDriver interface {
	DialL4(ctx context.Context, network, addr string, opts DialOptions) (net.Conn, error)
}

// L2FrameConn wraps an L2Device and adapts frame reads/writes to satisfy the net.Conn interface.
type L2FrameConn struct {
	dev        L2Device
	localAddr  net.Addr
	remoteAddr net.Addr
}

// NewL2FrameConn wraps an L2Device into a net.Conn compatible frame stream.
func NewL2FrameConn(dev L2Device, localAddr, remoteAddr net.Addr) *L2FrameConn {
	return &L2FrameConn{
		dev:        dev,
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
	}
}

func (c *L2FrameConn) Read(b []byte) (int, error) {
	if c.dev == nil {
		return 0, ErrL2DeviceNil
	}

	return c.dev.ReadFrame(b)
}

func (c *L2FrameConn) Write(b []byte) (int, error) {
	if c.dev == nil {
		return 0, ErrL2DeviceNil
	}

	return c.dev.WriteFrame(b)
}

func (c *L2FrameConn) Close() error {
	if c.dev == nil {
		return nil
	}

	return c.dev.Close()
}

func (c *L2FrameConn) LocalAddr() net.Addr {
	if c.localAddr != nil {
		return c.localAddr
	}

	if c.dev != nil && c.dev.HardwareAddr() != nil {
		return &L2Addr{HardwareAddr: c.dev.HardwareAddr()}
	}

	return &L2Addr{}
}

func (c *L2FrameConn) RemoteAddr() net.Addr {
	if c.remoteAddr != nil {
		return c.remoteAddr
	}

	return &L2Addr{}
}

func (c *L2FrameConn) SetDeadline(t time.Time) error      { return nil }
func (c *L2FrameConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *L2FrameConn) SetWriteDeadline(t time.Time) error { return nil }

var _ net.Conn = (*L2FrameConn)(nil)
