// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

// ClientOption is a functional option that configures a [Config] and is
// consumed by [NewClient] or [Client.With] to produce a configured client.
// Concrete option implementations are located in the [github.com/lemon4ksan/aoni/option] package.
type ClientOption func(cfg *Config)
