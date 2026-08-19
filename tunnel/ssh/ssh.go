// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ssh provides an enterprise-grade SSH tunneling, command execution,
// SFTP transfer, and SSH server framework fully integrated with aoni's L4 transport pipeline.
package ssh

import (
	"github.com/lemon4ksan/aoni/tunnel/ssh/ca"
	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
	"github.com/lemon4ksan/aoni/tunnel/ssh/server"
	"github.com/lemon4ksan/aoni/tunnel/ssh/tarpit"
)

// Type aliases for Client operations.
type (
	Client    = client.Client
	Cmd       = client.Cmd
	Option    = client.Option
	CmdOption = client.CmdOption
)

// Type aliases for Server operations.
type (
	Server           = server.Server
	Session          = server.Session
	Context          = server.Context
	ServerHandler    = server.Handler
	ServerOption     = server.Option
	SubsystemHandler = server.SubsystemHandler
)

// DefaultWindowSize specifies the default initial SSH channel window size (16MB).
const DefaultWindowSize uint32 = client.DefaultWindowSize

// DefaultMaxPacketSize specifies the default maximum SSH packet size (64KB).
const DefaultMaxPacketSize uint32 = client.DefaultMaxPacketSize

// NewClient establishes an SSH connection wrapped in aoni's transport pipeline.
var NewClient = client.New

// DialContext connects client to the remote target host using configured options, proxies, or jump hosts.
var DialContext = client.Dial

// NewServer creates a new SSH server with optional options.
var NewServer = server.New

// Type aliases for CA operations.
type (
	CA          = ca.CA
	IssueConfig = ca.IssueConfig
	IssueOption = ca.IssueOption
	OIDCConfig  = ca.OIDCConfig
)

var (
	// NewCA instantiates an SSH Certificate Authority.
	NewCA = ca.NewCA
	// GenerateCA generates a new SSH CA key pair and returns the CA and PEM-encoded private key.
	GenerateCA = ca.GenerateCA
)

var (
	// TarpitTrap traps incoming connections and applies tarpit mitigation.
	TarpitTrap = tarpit.Trap
	// ZeroWindowFreeze applies zero window freeze mitigation to incoming connections.
	ZeroWindowFreeze = tarpit.ZeroWindowFreeze
)

type (
	SSHConfig  = client.SSHConfig
	HostConfig = client.HostConfig
)

var (
	NewClientFromConfig = client.NewClientFromConfig
	ParseSSHConfig      = client.ParseSSHConfig
	ParseSSHConfigFile  = client.ParseSSHConfigFile
)
