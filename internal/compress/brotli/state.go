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
	stateMetablockHeaderNone = iota
	stateMetablockHeaderEmpty
	stateMetablockHeaderNibbles
	stateMetablockHeaderSize
	stateMetablockHeaderUncompressed
	stateMetablockHeaderReserved
	stateMetablockHeaderBytes
	stateMetablockHeaderMetadata
)

const (
	stateUncompressedNone = iota
	stateUncompressedWrite
)

const (
	stateTreeGroupNone = iota
	stateTreeGroupLoop
)

const (
	stateContextMapNone = iota
	stateContextMapReadPrefix
	stateContextMapHuffman
	stateContextMapDecode
	stateContextMapTransform
)

const (
	stateHuffmanNone = iota
	stateHuffmanSimpleSize
	stateHuffmanSimpleRead
	stateHuffmanSimpleBuild
	stateHuffmanComplex
	stateHuffmanLengthSymbols
)

const (
	stateDecodeUint8None = iota
	stateDecodeUint8Short
	stateDecodeUint8Long
)

const (
	stateReadBlockLengthNone = iota
	stateReadBlockLengthSuffix
)

type Reader struct {
	src io.Reader
	buf []byte // scratch space for reading from src
	in  []byte // current chunk to decode; usually aliases buf

	state       int
	loopCounter int
	br          bitReader
	buffer      struct {
		u64 uint64
		u8  [8]byte
	}
	bufferLength              uint32
	pos                       int
	maxBackwardDistance       int
	maxDistance               int
	ringbufferSize            int
	ringbufferMask            int
	distRbIdx                 int
	distRb                    [4]int
	errorCode                 int
	subLoopCounter            uint32
	ringbuffer                []byte
	ringbufferEnd             []byte
	htreeCommand              []huffmanCode
	contextLookup             contextLUT
	contextMapSlice           []byte
	distContextMapSlice       []byte
	literalHgroup             huffmanTreeGroup
	insertCopyHgroup          huffmanTreeGroup
	distanceHgroup            huffmanTreeGroup
	blockTypeTrees            []huffmanCode
	blockLenTrees             []huffmanCode
	trivialLiteralContext     int
	distanceContext           int
	metaBlockRemainingLen     int
	blockLengthIndex          uint32
	blockLength               [3]uint32
	numBlockTypes             [3]uint32
	blockTypeRB               [6]uint32
	distancePostfixBits       uint32
	numDirectDistanceCodes    uint32
	distancePostfixMask       int
	numDistHtrees             uint32
	distContextMap            []byte
	literalHtree              []huffmanCode
	distHtreeIndex            byte
	repeatCodeLen             uint32
	prevCodeLen               uint32
	copyLength                int
	distanceCode              int
	rbRoundtrips              uint
	partialPosOut             uint
	symbol                    uint32
	repeat                    uint32
	space                     uint32
	table                     [32]huffmanCode
	symbolLists               symbolList
	symbolsListsArray         [huffmanMaxCodeLength + 1 + numCommandSymbols]uint16
	nextSymbol                [32]int
	codeLengthCodeLengths     [codeLengthCodes]byte
	codeLengthHisto           [16]uint16
	htreeIndex                int
	next                      []huffmanCode
	contextIndex              uint32
	maxRunLengthPrefix        uint32
	code                      uint32
	contextMapTable           [huffmanMaxSize272]huffmanCode
	substateMetablockHeader   int
	substateTreeGroup         int
	substateContextMap        int
	substateUncompressed      int
	substateHuffman           int
	substateDecodeUint8       int
	substateReadBlockLength   int
	isLastMetablock           uint
	isUncompressed            uint
	isMetadata                uint
	shouldWrapRingbuffer      uint
	cannyRingbufferAllocation uint
	largeWindow               bool
	sizeNibbles               uint
	windowBits                uint32
	newRingbufferSize         int
	numLiteralHtrees          uint32
	contextMap                []byte
	contextModes              []byte
	dictionary                *dictionary
	transforms                *transforms
	trivialLiteralContexts    [8]uint32
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

	s.isLastMetablock = 0
	s.isUncompressed = 0
	s.isMetadata = 0
	s.shouldWrapRingbuffer = 0
	s.cannyRingbufferAllocation = 1

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
