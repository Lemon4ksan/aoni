// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

// KeyPhase is the key phase
type KeyPhase uint64

// Bit determines the key phase bit
func (p KeyPhase) Bit() KeyPhaseBit {
	if p%2 == 0 {
		return KeyPhaseZero
	}

	return KeyPhaseOne
}

// KeyPhaseBit is the key phase bit
type KeyPhaseBit uint8

const (
	// KeyPhaseUndefined is an undefined key phase
	KeyPhaseUndefined KeyPhaseBit = iota
	// KeyPhaseZero is key phase 0
	KeyPhaseZero
	// KeyPhaseOne is key phase 1
	KeyPhaseOne
)

func (p KeyPhaseBit) String() string {
	//nolint:exhaustive
	switch p {
	case KeyPhaseZero:
		return "0"
	case KeyPhaseOne:
		return "1"
	default:
		return "undefined"
	}
}
