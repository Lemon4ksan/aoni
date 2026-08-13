// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package h1parser implements an Nginx-style single-pass DFA state machine for parsing HTTP/1.x requests.
package h1parser

import "github.com/lemon4ksan/aoni/codec"

type dfaState uint8

const (
	stateStart dfaState = iota
	stateMethod
	stateSpacesBeforeURI
	stateURI
	stateSpacesBeforeProto
	stateProto
	stateLineEndLF
	stateHeaderKey
	stateHeaderColon
	stateHeaderValSpace
	stateHeaderVal
	stateHeaderEndLF
	stateDone
)

// ParseHTTP1SinglePass parses raw socket bytes using an Nginx-style single-pass DFA state machine.
// Returns method, URI, proto, and HTX header index in a single pass over memory.
func ParseHTTP1SinglePass(
	buf []byte,
) (method, uri, proto []byte, htx codec.RequestHeaderIndex, bytesParsed int, ok bool) {
	state := stateStart

	var (
		mStart, mEnd int
		uStart, uEnd int
		pStart, pEnd int
		kStart, kEnd int
		vStart, vEnd int
	)

	n := len(buf)
	for i := 0; i < n; i++ {
		b := buf[i]

		switch state {
		case stateStart:
			if b != ' ' && b != '\r' && b != '\n' {
				mStart = i
				state = stateMethod
			}

		case stateMethod:
			if b == ' ' {
				mEnd = i
				state = stateSpacesBeforeURI
			}

		case stateSpacesBeforeURI:
			if b != ' ' {
				uStart = i
				state = stateURI
			}

		case stateURI:
			switch b {
			case ' ':
				uEnd = i
				state = stateSpacesBeforeProto
			case '\r', '\n':
				uEnd = i
				state = stateLineEndLF
			}

		case stateSpacesBeforeProto:
			if b != ' ' {
				pStart = i
				state = stateProto
			}

		case stateProto:
			switch b {
			case '\r':
				pEnd = i
				state = stateLineEndLF
			case '\n':
				pEnd = i
				kStart = i + 1
				state = stateHeaderKey
			}

		case stateLineEndLF:
			if b == '\n' {
				kStart = i + 1
				state = stateHeaderKey
			}

		case stateHeaderKey:
			switch b {
			case ':':
				kEnd = i
				state = stateHeaderColon
			case '\r', '\n':
				// Empty line -> end of headers
				bytesParsed = i + 1

				return buf[mStart:mEnd], buf[uStart:uEnd], buf[pStart:pEnd], htx, bytesParsed, true
			}

		case stateHeaderColon:
			if b == ' ' || b == '\t' {
				state = stateHeaderValSpace
			} else {
				vStart = i
				state = stateHeaderVal
			}

		case stateHeaderValSpace:
			if b != ' ' && b != '\t' {
				vStart = i
				state = stateHeaderVal
			}

		case stateHeaderVal:
			switch b {
			case '\r':
				vEnd = i
				htx.AddSlot(kStart, kEnd-kStart, vStart, vEnd-vStart)

				state = stateHeaderEndLF
			case '\n':
				vEnd = i
				htx.AddSlot(kStart, kEnd-kStart, vStart, vEnd-vStart)
				kStart = i + 1
				state = stateHeaderKey
			}

		case stateHeaderEndLF:
			if b == '\n' {
				kStart = i + 1
				state = stateHeaderKey
			}
		}
	}

	return nil, nil, nil, htx, 0, false
}
