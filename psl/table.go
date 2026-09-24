// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package psl

import _ "embed"

const version = "publicsuffix.org's public_suffix_list.dat, git revision d6c92f1bbb7433e5db7b8405c25d4035fb8ff376"

const (
	nodesBits           = 40
	nodesBitsChildren   = 10
	nodesBitsICANN      = 1
	nodesBitsTextOffset = 16
	nodesBitsTextLength = 6

	childrenBitsWildcard = 1
	childrenBitsNodeType = 2
	childrenBitsHi       = 14
	childrenBitsLo       = 14
)

const (
	nodeTypeNormal     = 0
	nodeTypeException  = 1
	nodeTypeParentOnly = 2
)

// numTLD is the number of top level domains.
const numTLD = 1450

// text is the combined text of all labels.
//
//go:embed data/text
var text string

// nodes is the list of nodes.
//
//go:embed data/nodes
var nodes uint40String

// children is the list of nodes' children.
//
//go:embed data/children
var children uint32String

type uint32String string

func (u uint32String) get(i uint32) uint32 {
	off := i * 4
	u = u[off:]
	return uint32(u[3]) |
		uint32(u[2])<<8 |
		uint32(u[1])<<16 |
		uint32(u[0])<<24
}

type uint40String string

func (u uint40String) get(i uint32) uint64 {
	off := uint64(i * (nodesBits / 8))
	u = u[off:]
	return uint64(u[4]) |
		uint64(u[3])<<8 |
		uint64(u[2])<<16 |
		uint64(u[1])<<24 |
		uint64(u[0])<<32
}
