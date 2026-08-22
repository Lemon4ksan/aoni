// Copyright 2013 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

// RFC 7932 transform operation types.
const (
	transformIdentity       = 0
	transformOmitLast1      = 1
	transformOmitLast2      = 2
	transformOmitLast3      = 3
	transformOmitLast4      = 4
	transformOmitLast5      = 5
	transformOmitLast6      = 6
	transformOmitLast7      = 7
	transformOmitLast8      = 8
	transformOmitLast9      = 9
	transformUppercaseFirst = 10
	transformUppercaseAll   = 11
	transformOmitFirst1     = 12
	transformOmitFirst2     = 13
	transformOmitFirst3     = 14
	transformOmitFirst4     = 15
	transformOmitFirst5     = 16
	transformOmitFirst6     = 17
	transformOmitFirst7     = 18
	transformOmitFirst8     = 19
	transformOmitFirst9     = 20
	transformShiftFirst     = 21
	transformShiftAll       = 22
	numTransformTypes       = 23
)

const transformsMaxCutOff = transformOmitLast9

type transforms struct {
	prefixSuffixSize uint16
	prefixSuffix     []byte
	prefixSuffixMap  []uint16
	numTransforms    uint32
	transforms       []byte
	params           []byte
	cutOffTransforms [transformsMaxCutOff + 1]int16
}

func (t *transforms) prefixID(i int) byte {
	return t.transforms[(i*3)+0]
}

func (t *transforms) transformType(i int) byte {
	return t.transforms[(i*3)+1]
}

func (t *transforms) suffixID(i int) byte {
	return t.transforms[(i*3)+2]
}

func (t *transforms) prefix(i int) []byte {
	return t.prefixSuffix[t.prefixSuffixMap[t.prefixID(i)]:]
}

func (t *transforms) suffix(i int) []byte {
	return t.prefixSuffix[t.prefixSuffixMap[t.suffixID(i)]:]
}

const kPrefixSuffix string = "\001 \002, \010 of the \004 of \002s \001.\005 and \004 " +
	"in \001\"\004 to \002\">\001\n\002. \001]\005 for \003 a \006 " +
	"that \001'\006 with \006 from \004 by \001(\006. T" +
	"he \004 on \004 as \004 is \004ing \002\n\t\001:\003ed " +
	"\002=\"\004 at \003ly \001,\002='\005.com/\007. This \005" +
	" not \003er \003al \004ful \004ive \005less \004es" +
	"t \004ize \002\xc2\xa0\004ous \005 the \002e \000"

var kPrefixSuffixMap = [50]uint16{
	0x00, 0x02, 0x05, 0x0E, 0x13, 0x16, 0x18, 0x1E, 0x23, 0x25,
	0x2A, 0x2D, 0x2F, 0x32, 0x34, 0x3A, 0x3E, 0x45, 0x47, 0x4E,
	0x55, 0x5A, 0x5C, 0x63, 0x68, 0x6D, 0x72, 0x77, 0x7A, 0x7C,
	0x80, 0x83, 0x88, 0x8C, 0x8E, 0x91, 0x97, 0x9F, 0xA5, 0xA9,
	0xAD, 0xB2, 0xB7, 0xBD, 0xC2, 0xC7, 0xCA, 0xCF, 0xD5, 0xD8,
}

// RFC 7932 static transformation rules.
var kTransformsData = []byte{
	49,
	transformIdentity,
	49,
	49,
	transformIdentity,
	0,
	0,
	transformIdentity,
	0,
	49,
	transformOmitFirst1,
	49,
	49,
	transformUppercaseFirst,
	0,
	49,
	transformIdentity,
	47,
	0,
	transformIdentity,
	49,
	4,
	transformIdentity,
	0,
	49,
	transformIdentity,
	3,
	49,
	transformUppercaseFirst,
	49,
	49,
	transformIdentity,
	6,
	49,
	transformOmitFirst2,
	49,
	49,
	transformOmitLast1,
	49,
	1,
	transformIdentity,
	0,
	49,
	transformIdentity,
	1,
	0,
	transformUppercaseFirst,
	0,
	49,
	transformIdentity,
	7,
	49,
	transformIdentity,
	9,
	48,
	transformIdentity,
	0,
	49,
	transformIdentity,
	8,
	49,
	transformIdentity,
	5,
	49,
	transformIdentity,
	10,
	49,
	transformIdentity,
	11,
	49,
	transformOmitLast3,
	49,
	49,
	transformIdentity,
	13,
	49,
	transformIdentity,
	14,
	49,
	transformOmitFirst3,
	49,
	49,
	transformOmitLast2,
	49,
	49,
	transformIdentity,
	15,
	49,
	transformIdentity,
	16,
	0,
	transformUppercaseFirst,
	49,
	49,
	transformIdentity,
	12,
	5,
	transformIdentity,
	49,
	0,
	transformIdentity,
	1,
	49,
	transformOmitFirst4,
	49,
	49,
	transformIdentity,
	18,
	49,
	transformIdentity,
	17,
	49,
	transformIdentity,
	19,
	49,
	transformIdentity,
	20,
	49,
	transformOmitFirst5,
	49,
	49,
	transformOmitFirst6,
	49,
	47,
	transformIdentity,
	49,
	49,
	transformOmitLast4,
	49,
	49,
	transformIdentity,
	22,
	49,
	transformUppercaseAll,
	49,
	49,
	transformIdentity,
	23,
	49,
	transformIdentity,
	24,
	49,
	transformIdentity,
	25,
	49,
	transformOmitLast7,
	49,
	49,
	transformOmitLast1,
	26,
	49,
	transformIdentity,
	27,
	49,
	transformIdentity,
	28,
	0,
	transformIdentity,
	12,
	49,
	transformIdentity,
	29,
	49,
	transformOmitFirst9,
	49,
	49,
	transformOmitFirst7,
	49,
	49,
	transformOmitLast6,
	49,
	49,
	transformIdentity,
	21,
	49,
	transformUppercaseFirst,
	1,
	49,
	transformOmitLast8,
	49,
	49,
	transformIdentity,
	31,
	49,
	transformIdentity,
	32,
	47,
	transformIdentity,
	3,
	49,
	transformOmitLast5,
	49,
	49,
	transformOmitLast9,
	49,
	0,
	transformUppercaseFirst,
	1,
	49,
	transformUppercaseFirst,
	8,
	5,
	transformIdentity,
	21,
	49,
	transformUppercaseAll,
	0,
	49,
	transformUppercaseFirst,
	10,
	49,
	transformIdentity,
	30,
	0,
	transformIdentity,
	5,
	35,
	transformIdentity,
	49,
	47,
	transformIdentity,
	2,
	49,
	transformUppercaseFirst,
	17,
	49,
	transformIdentity,
	36,
	49,
	transformIdentity,
	33,
	5,
	transformIdentity,
	0,
	49,
	transformUppercaseFirst,
	21,
	49,
	transformUppercaseFirst,
	5,
	49,
	transformIdentity,
	37,
	0,
	transformIdentity,
	30,
	49,
	transformIdentity,
	38,
	0,
	transformUppercaseAll,
	0,
	49,
	transformIdentity,
	39,
	0,
	transformUppercaseAll,
	49,
	49,
	transformIdentity,
	34,
	49,
	transformUppercaseAll,
	8,
	49,
	transformUppercaseFirst,
	12,
	0,
	transformIdentity,
	21,
	49,
	transformIdentity,
	40,
	0,
	transformUppercaseFirst,
	12,
	49,
	transformIdentity,
	41,
	49,
	transformIdentity,
	42,
	49,
	transformUppercaseAll,
	17,
	49,
	transformIdentity,
	43,
	0,
	transformUppercaseFirst,
	5,
	49,
	transformUppercaseAll,
	10,
	0,
	transformIdentity,
	34,
	49,
	transformUppercaseFirst,
	33,
	49,
	transformIdentity,
	44,
	49,
	transformUppercaseAll,
	5,
	45,
	transformIdentity,
	49,
	0,
	transformIdentity,
	33,
	49,
	transformUppercaseFirst,
	30,
	49,
	transformUppercaseAll,
	30,
	49,
	transformIdentity,
	46,
	49,
	transformUppercaseAll,
	1,
	49,
	transformUppercaseFirst,
	34,
	0,
	transformUppercaseFirst,
	33,
	0,
	transformUppercaseAll,
	30,
	0,
	transformUppercaseAll,
	1,
	49,
	transformUppercaseAll,
	33,
	49,
	transformUppercaseAll,
	21,
	49,
	transformUppercaseAll,
	12,
	0,
	transformUppercaseAll,
	5,
	49,
	transformUppercaseAll,
	34,
	0,
	transformUppercaseAll,
	12,
	0,
	transformUppercaseFirst,
	30,
	0,
	transformUppercaseAll,
	34,
	0,
	transformUppercaseFirst,
	34,
}

var kTransforms = transforms{
	prefixSuffixSize: uint16(len(kPrefixSuffix)),
	prefixSuffix:     []byte(kPrefixSuffix),
	prefixSuffixMap:  kPrefixSuffixMap[:],
	numTransforms:    uint32(len(kTransformsData) / 3),
	transforms:       kTransformsData,
	params:           nil,
	cutOffTransforms: [transformsMaxCutOff + 1]int16{
		-1, 11, -1, 23, -1, -1, -1, 44, -1, 102,
	},
}

func getTransforms() *transforms {
	return &kTransforms
}

func toUpperCase(p []byte) int {
	if p[0] < 0xC0 {
		if p[0] >= 'a' && p[0] <= 'z' {
			p[0] ^= 32
		}

		return 1
	}

	if p[0] < 0xE0 {
		p[1] ^= 32
		return 2
	}

	p[2] ^= 5

	return 3
}

func shiftTransform(word []byte, wordLen int, parameter uint16) int {
	scalar := (uint32(parameter) & 0x7FFF) + (0x1000000 - (uint32(parameter) & 0x8000))
	switch {
	case word[0] < 0x80:
		scalar += uint32(word[0])
		word[0] = byte(scalar & 0x7F)
		return 1
	case word[0] < 0xC0:
		return 1
	case word[0] < 0xE0:
		if wordLen < 2 {
			return 1
		}

		scalar += uint32(word[1]&0x3F | (word[0]&0x1F)<<6)
		word[0] = byte(0xC0 | (scalar>>6)&0x1F)
		word[1] = byte(uint32(word[1]&0xC0) | scalar&0x3F)

		return 2

	case word[0] < 0xF0:
		if wordLen < 3 {
			return wordLen
		}

		scalar += uint32(word[2])&0x3F | uint32(word[1]&0x3F)<<6 | uint32(word[0]&0x0F)<<12
		word[0] = byte(0xE0 | (scalar>>12)&0x0F)
		word[1] = byte(uint32(word[1]&0xC0) | (scalar>>6)&0x3F)
		word[2] = byte(uint32(word[2]&0xC0) | scalar&0x3F)

		return 3

	case word[0] < 0xF8:
		if wordLen < 4 {
			return wordLen
		}

		scalar += uint32(word[3])&0x3F | uint32(word[2]&0x3F)<<6 | uint32(word[1]&0x3F)<<12 | uint32(word[0]&0x07)<<18
		word[0] = byte(0xF0 | (scalar>>18)&0x07)
		word[1] = byte(uint32(word[1]&0xC0) | (scalar>>12)&0x3F)
		word[2] = byte(uint32(word[2]&0xC0) | (scalar>>6)&0x3F)
		word[3] = byte(uint32(word[3]&0xC0) | scalar&0x3F)

		return 4

	default:
		return 1
	}
}

func transformDictionaryWord(dst, word []byte, wordLen int, trans *transforms, transformIdx int) int {
	idx := 0
	prefix := trans.prefix(transformIdx)
	tType := int(trans.transformType(transformIdx))
	suffix := trans.suffix(transformIdx)

	prefixLen := int(prefix[0])

	prefix = prefix[1:]
	for prefixLen > 0 {
		dst[idx] = prefix[0]
		idx++
		prefix = prefix[1:]
		prefixLen--
	}

	if tType <= transformOmitLast9 {
		wordLen -= tType
	} else if tType >= transformOmitFirst1 && tType <= transformOmitFirst9 {
		skip := tType - (transformOmitFirst1 - 1)
		word = word[skip:]
		wordLen -= skip
	}

	for i := 0; i < wordLen; i++ {
		dst[idx] = word[i]
		idx++
	}

	switch tType {
	case transformUppercaseFirst:
		toUpperCase(dst[idx-wordLen:])
	case transformUppercaseAll:
		uppercase := dst[idx-wordLen:]
		for wordLen > 0 {
			step := toUpperCase(uppercase)
			uppercase = uppercase[step:]
			wordLen -= step
		}

	case transformShiftFirst:
		param := uint16(trans.params[transformIdx*2]) + uint16(trans.params[transformIdx*2+1])<<8
		shiftTransform(dst[idx-wordLen:], wordLen, param)
	case transformShiftAll:
		param := uint16(trans.params[transformIdx*2]) + uint16(trans.params[transformIdx*2+1])<<8

		shift := dst[idx-wordLen:]
		for wordLen > 0 {
			step := shiftTransform(shift, wordLen, param)
			shift = shift[step:]
			wordLen -= step
		}
	}

	suffixLen := int(suffix[0])

	suffix = suffix[1:]
	for suffixLen > 0 {
		dst[idx] = suffix[0]
		idx++
		suffix = suffix[1:]
		suffixLen--
	}

	return idx
}
