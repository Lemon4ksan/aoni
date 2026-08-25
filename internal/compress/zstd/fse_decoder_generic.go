// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || appengine || !gc || noasm

package zstd

import (
	"errors"
	"fmt"
)

func tableStep(tableSize uint32) uint32 {
	return (tableSize >> 1) + (tableSize >> 3) + 3
}

func (d *decSymbol) setAddBits(addBits uint8) {
	*d = (*d &^ 0xff00) | (decSymbol(addBits) << 8)
}

func (d *decSymbol) setNBits(nBits uint8) {
	*d = (*d &^ 0xff) | decSymbol(nBits)
}

func (d *decSymbol) setNewState(newState uint16) {
	*d = (*d &^ 0xffff0000) | (decSymbol(newState) << 16)
}

// buildDtable will build the decoding table.
func (s *fseDecoder) buildDtable() error {
	tableSize := uint32(1 << s.actualTableLog)
	highThreshold := tableSize - 1
	symbolNext := s.stateTable[:256]

	// Init, lay down lowprob symbols
	{
		for i, v := range s.norm[:s.symbolLen] {
			if v == -1 {
				s.dt[highThreshold].setAddBits(uint8(i))
				highThreshold--
				v = 1
			}
			symbolNext[i] = uint16(v)
		}
	}

	// Spread symbols
	{
		tableMask := tableSize - 1
		step := tableStep(tableSize)
		position := uint32(0)
		for ss, v := range s.norm[:s.symbolLen] {
			for i := 0; i < int(v); i++ {
				s.dt[position].setAddBits(uint8(ss))
				for {
					// lowprob area
					position = (position + step) & tableMask
					if position <= highThreshold {
						break
					}
				}
			}
		}
		if position != 0 {
			// position must reach all cells once, otherwise normalizedCounter is incorrect
			return errors.New("corrupted input (position != 0)")
		}
	}

	// Build Decoding table
	{
		tableSize := uint16(1 << s.actualTableLog)
		for u, v := range s.dt[:tableSize] {
			symbol := v.addBits()
			nextState := symbolNext[symbol]
			symbolNext[symbol] = nextState + 1
			nBits := s.actualTableLog - byte(highBits(uint32(nextState)))
			s.dt[u&maxTableMask].setNBits(nBits)
			newState := (nextState << nBits) - tableSize
			if newState > tableSize {
				return fmt.Errorf("newState (%d) outside table size (%d)", newState, tableSize)
			}
			if newState == uint16(u) && nBits == 0 {
				// Seems weird that this is possible with nbits > 0.
				return fmt.Errorf("newState (%d) == oldState (%d) and no bits", newState, u)
			}
			s.dt[u&maxTableMask].setNewState(newState)
		}
	}
	return nil
}
