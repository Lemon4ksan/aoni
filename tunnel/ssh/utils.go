// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

import (
	"github.com/lemon4ksan/aoni/tunnel/ssh/agent"
	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
)

var (
	HasAgent              = agent.HasAgent
	ParseKeyFile          = client.ParseKeyFile
	ParseKey              = client.ParseKey
	ParseCertKey          = client.ParseCertKey
	ParseCertKeyFile      = client.ParseCertKeyFile
	DefaultKnownHosts     = client.DefaultKnownHosts
	KnownHosts            = client.KnownHosts
	EnsureKnownHosts      = client.EnsureKnownHosts
	CheckKnownHost        = client.CheckKnownHost
	AddKnownHost          = client.AddKnownHost
	DefaultKnownHostsPath = client.DefaultKnownHostsPath
)
