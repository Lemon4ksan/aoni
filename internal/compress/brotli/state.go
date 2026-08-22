// Copyright 2013 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

import "io"

/* Brotli state for partial streaming decoding. */
const (
	stateUninited = iota
	stateLargeWindowBits
	stateInitialize
	stateMetablockBegin
	stateMetablockHeader
	stateMetablockHeader2
	stateContextModes
	stateCommandBegin
	stateCommandInner
	stateCommandPostDecodeLiterals
	stateCommandPostWrapCopy
	stateUncompressed
	stateMetadata
	stateCommandInnerWrite
	stateMetablockDone
	stateCommandPostWrite1
	stateCommandPostWrite2
	stateHuffmanCode0
	stateHuffmanCode1
	stateHuffmanCode2
	stateHuffmanCode3
	stateContextMap1
	stateContextMap2
	stateTreeGroup
	stateDone
)

const (
	stateMetablockHeaderNone uint8 = iota
	stateMetablockHeaderEmpty
	stateMetablockHeaderNibbles
	stateMetablockHeaderSize
	stateMetablockHeaderUncompressed
	stateMetablockHeaderReserved
	stateMetablockHeaderBytes
	stateMetablockHeaderMetadata
)

const (
	stateUncompressedNone uint8 = iota
	stateUncompressedWrite
)

const (
	stateTreeGroupNone uint8 = iota
	stateTreeGroupLoop
)

const (
	stateContextMapNone uint8 = iota
	stateContextMapReadPrefix
	stateContextMapHuffman
	stateContextMapDecode
	stateContextMapTransform
)

const (
	stateHuffmanNone uint8 = iota
	stateHuffmanSimpleSize
	stateHuffmanSimpleRead
	stateHuffmanSimpleBuild
	stateHuffmanComplex
	stateHuffmanLengthSymbols
)

const (
	stateDecodeUint8None uint8 = iota
	stateDecodeUint8Short
	stateDecodeUint8Long
)

const (
	stateReadBlockLengthNone uint8 = iota
	stateReadBlockLengthSuffix
)

type Reader struct {
	// Source and buffer slices
	src                 io.Reader
	buf                 []byte // scratch space for reading from src
	in                  []byte // current chunk to decode; usually aliases buf
	ringbuffer          []byte
	ringbufferEnd       []byte
	htreeCommand        []huffmanCode
	contextLookup       contextLUT
	contextMapSlice     []byte
	distContextMapSlice []byte
	blockTypeTrees      []huffmanCode
	blockLenTrees       []huffmanCode
	distContextMap      []byte
	literalHtree        []huffmanCode
	next                []huffmanCode
	contextMap          []byte
	contextModes        []byte

	dictionary *dictionary
	transforms *transforms

	literalHgroup    huffmanTreeGroup
	insertCopyHgroup huffmanTreeGroup
	distanceHgroup   huffmanTreeGroup

	symbolLists symbolList
	br          bitReader

	buffer struct {
		u64 uint64
		u8  [8]byte
	}

	// 64-bit / int fields
	state                 int
	loopCounter           int
	pos                   int
	maxBackwardDistance   int
	maxDistance           int
	ringbufferSize        int
	newRingbufferSize     int
	ringbufferMask        int
	distRbIdx             int
	errorCode             int
	trivialLiteralContext int
	distanceContext       int
	metaBlockRemainingLen int
	distancePostfixMask   int
	copyLength            int
	distanceCode          int
	htreeIndex            int
	rbRoundtrips          uint
	partialPosOut         uint
	distRb                [4]int
	nextSymbol            [32]int

	// 32-bit / uint32 fields
	bufferLength           uint32
	subLoopCounter         uint32
	blockLengthIndex       uint32
	distancePostfixBits    uint32
	numDirectDistanceCodes uint32
	numDistHtrees          uint32
	repeatCodeLen          uint32
	prevCodeLen            uint32
	symbol                 uint32
	repeat                 uint32
	space                  uint32
	contextIndex           uint32
	maxRunLengthPrefix     uint32
	code                   uint32
	windowBits             uint32
	numLiteralHtrees       uint32
	blockLength            [3]uint32
	numBlockTypes          [3]uint32
	blockTypeRB            [6]uint32
	trivialLiteralContexts [8]uint32

	// Tables / Arrays
	table                 [32]huffmanCode
	contextMapTable       [huffmanMaxSize272]huffmanCode
	symbolsListsArray     [huffmanMaxCodeLength + 1 + numCommandSymbols]uint16
	codeLengthHisto       [16]uint16
	codeLengthCodeLengths [codeLengthCodes]byte

	// 8-bit substates
	substateMetablockHeader uint8
	substateTreeGroup       uint8
	substateContextMap      uint8
	substateUncompressed    uint8
	substateHuffman         uint8
	substateDecodeUint8     uint8
	substateReadBlockLength uint8
	distHtreeIndex          byte
	sizeNibbles             uint8

	// Boolean flags
	isLastMetablock           bool
	isUncompressed            bool
	isMetadata                bool
	shouldWrapRingbuffer      bool
	cannyRingbufferAllocation bool
	largeWindow               bool
}

func (s *Reader) initState() bool {
	s.errorCode = 0 /* BROTLI_DECODER_NO_ERROR */

	s.br.init()
	s.state = stateUninited
	s.largeWindow = false
	s.substateMetablockHeader = stateMetablockHeaderNone
	s.substateTreeGroup = stateTreeGroupNone
	s.substateContextMap = stateContextMapNone
	s.substateUncompressed = stateUncompressedNone
	s.substateHuffman = stateHuffmanNone
	s.substateDecodeUint8 = stateDecodeUint8None
	s.substateReadBlockLength = stateReadBlockLengthNone

	s.bufferLength = 0
	s.loopCounter = 0
	s.pos = 0
	s.rbRoundtrips = 0
	s.partialPosOut = 0

	clear(s.blockTypeTrees)
	s.blockLenTrees = nil
	s.ringbufferSize = 0
	s.newRingbufferSize = 0
	s.ringbufferMask = 0

	s.contextMap = nil
	s.contextModes = nil
	s.distContextMap = nil
	s.contextMapSlice = nil
	s.distContextMapSlice = nil

	s.subLoopCounter = 0

	s.cleanupCodes()
	s.cleanupHTrees()

	s.isLastMetablock = false
	s.isUncompressed = false
	s.isMetadata = false
	s.shouldWrapRingbuffer = false
	s.cannyRingbufferAllocation = true

	s.windowBits = 0
	s.maxDistance = 0
	s.distRb[0] = 16
	s.distRb[1] = 15
	s.distRb[2] = 11
	s.distRb[3] = 4
	s.distRbIdx = 0

	s.symbolLists.storage = s.symbolsListsArray[:]
	s.symbolLists.offset = huffmanMaxCodeLength + 1

	s.dictionary = getDictionary()
	s.transforms = getTransforms()

	return true
}

func (s *Reader) metablockBegin() {
	s.metaBlockRemainingLen = 0
	s.blockLength[0] = 1 << 24
	s.blockLength[1] = 1 << 24
	s.blockLength[2] = 1 << 24
	s.numBlockTypes[0] = 1
	s.numBlockTypes[1] = 1
	s.numBlockTypes[2] = 1
	s.blockTypeRB[0] = 1
	s.blockTypeRB[1] = 0
	s.blockTypeRB[2] = 1
	s.blockTypeRB[3] = 0
	s.blockTypeRB[4] = 1
	s.blockTypeRB[5] = 0
	s.contextMap = nil
	s.contextModes = nil
	s.distContextMap = nil
	s.contextMapSlice = nil
	s.literalHtree = nil
	s.distContextMapSlice = nil
	s.distHtreeIndex = 0
	s.contextLookup = nil

	s.cleanupCodes()
	s.cleanupHTrees()
}

func (s *Reader) cleanupAfterMetablock() {
	s.contextModes = nil
	s.contextMap = nil
	s.distContextMap = nil
	s.cleanupHTrees()
}

func (group *huffmanTreeGroup) init(alphabetSize, maxSymbol, ntrees uint32) {
	maxTableSize := uint(kMaxHuffmanTableSize[(alphabetSize+31)>>5])

	group.alphabetSize = uint16(alphabetSize)
	group.maxSymbol = uint16(maxSymbol)
	group.numHtrees = uint16(ntrees)

	group.makeCodesBuffer(uint(ntrees) * maxTableSize)
	group.makeTreesBuffer(ntrees)
}

func (group *huffmanTreeGroup) makeTreesBuffer(treesLen uint32) {
	if cap(group.htrees) < int(treesLen) {
		group.htrees = make([][]huffmanCode, treesLen)
		return
	}

	group.htrees = group.htrees[:treesLen]
}

func (group *huffmanTreeGroup) makeCodesBuffer(codesLen uint) {
	if cap(group.codes) < int(codesLen) {
		group.codes = make([]huffmanCode, codesLen)
		return
	}

	group.codes = group.codes[:codesLen]
}

func (s *Reader) cleanupCodes() {
	clear(s.literalHgroup.codes)
	clear(s.insertCopyHgroup.codes)
	clear(s.distanceHgroup.codes)
}

func (s *Reader) cleanupHTrees() {
	clear(s.literalHgroup.htrees)
	clear(s.insertCopyHgroup.htrees)
	clear(s.distanceHgroup.htrees)
}
