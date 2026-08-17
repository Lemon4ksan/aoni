// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter

import (
	"bytes"
	"fmt"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

// // emitBitpack generates zero-allocation, register-optimized binary bitfield packing methods.
func emitBitpack(buf *bytes.Buffer, tracker *ImportTracker, bp *ir.BitpackIR) {
	if bp == nil {
		return
	}

	tracker.Add("encoding/binary")
	tracker.Add("errors")
	tracker.Add("fmt")

	name := bp.Name
	totalBits := bp.TotalBits
	totalBytes := bp.TotalBytes
	endianOrder := "binary.LittleEndian"

	if bp.Endianness == ir.EndianBig {
		endianOrder = "binary.BigEndian"
	}

	fmt.Fprintf(buf, "// %sBitSize is the total bit width of packed %s.\n", name, name)
	fmt.Fprintf(buf, "const %sBitSize = %d\n\n", name, totalBits)

	fmt.Fprintf(buf, "// %sByteSize is the total byte length of packed %s.\n", name, name)
	fmt.Fprintf(buf, "const %sByteSize = %d\n\n", name, totalBytes)

	hasBools := false

	for _, f := range bp.Fields {
		if f.IsBool {
			hasBools = true
			break
		}
	}

	if totalBits <= 64 {
		emitBitpackSingleWord(buf, bp, endianOrder, hasBools)
	} else {
		emitBitpackMultiWord(buf, bp, endianOrder, hasBools)
	}

	// Pack(dst []byte) ([]byte, error)
	fmt.Fprintf(buf, "// Pack appends the binary bit-packed representation of %s to dst and returns the slice.\n", name)
	fmt.Fprintf(buf, "func (s *%s) Pack(dst []byte) ([]byte, error) {\n", name)
	fmt.Fprintf(buf, "\tif s == nil {\n")
	fmt.Fprintf(buf, "\t\treturn nil, errors.New(\"aoni/bitpack: cannot pack nil *%s\")\n", name)
	fmt.Fprintf(buf, "\t}\n\n")
	fmt.Fprintf(buf, "\toffset := len(dst)\n")
	fmt.Fprintf(buf, "\tif cap(dst)-offset < %sByteSize {\n", name)
	fmt.Fprintf(buf, "\t\tnewDst := make([]byte, offset+%sByteSize, (offset+%sByteSize)*2)\n", name, name)
	fmt.Fprintf(buf, "\t\tcopy(newDst, dst)\n")
	fmt.Fprintf(buf, "\t\tdst = newDst\n")
	fmt.Fprintf(buf, "\t} else {\n")
	fmt.Fprintf(buf, "\t\tdst = dst[:offset+%sByteSize]\n", name)
	fmt.Fprintf(buf, "\t}\n\n")
	fmt.Fprintf(buf, "\ts.PackTo(dst[offset:])\n")
	fmt.Fprintf(buf, "\treturn dst, nil\n")
	fmt.Fprintf(buf, "}\n\n")

	// Unpack(src []byte) error
	fmt.Fprintf(buf, "// Unpack deserializes binary bit-packed data from src into s.\n")
	fmt.Fprintf(buf, "func (s *%s) Unpack(src []byte) error {\n", name)
	fmt.Fprintf(buf, "\tif s == nil {\n")
	fmt.Fprintf(buf, "\t\treturn errors.New(\"aoni/bitpack: cannot unpack into nil *%s\")\n", name)
	fmt.Fprintf(buf, "\t}\n")
	fmt.Fprintf(buf, "\tif len(src) < %sByteSize {\n", name)
	fmt.Fprintf(
		buf,
		"\t\treturn fmt.Errorf(\"aoni/bitpack: unexpected EOF, need %%d bytes but got %%d\", %sByteSize, len(src))\n",
		name,
	)
	fmt.Fprintf(buf, "\t}\n\n")
	fmt.Fprintf(buf, "\ts.UnpackFrom(src)\n")
	fmt.Fprintf(buf, "\treturn nil\n")
	fmt.Fprintf(buf, "}\n\n")

	// MarshalBinary and UnmarshalBinary
	fmt.Fprintf(buf, "// MarshalBinary implements encoding.BinaryMarshaler for %s.\n", name)
	fmt.Fprintf(buf, "func (s *%s) MarshalBinary() ([]byte, error) {\n", name)
	fmt.Fprintf(buf, "\treturn s.Pack(nil)\n")
	fmt.Fprintf(buf, "}\n\n")

	fmt.Fprintf(buf, "// UnmarshalBinary implements encoding.BinaryUnmarshaler for %s.\n", name)
	fmt.Fprintf(buf, "func (s *%s) UnmarshalBinary(data []byte) error {\n", name)
	fmt.Fprintf(buf, "\treturn s.Unpack(data)\n")
	fmt.Fprintf(buf, "}\n\n")

	// Batch Slice Pack and Unpack
	emitBitpackBatchSlice(buf, bp)
}

func emitBitpackSingleWord(buf *bytes.Buffer, bp *ir.BitpackIR, endianOrder string, hasBools bool) {
	name := bp.Name
	totalBytes := bp.TotalBytes

	// PackUint64
	fmt.Fprintf(buf, "// PackUint64 compiles all bitfields into a raw 64-bit integer register value.\n")
	fmt.Fprintf(buf, "func (s *%s) PackUint64() uint64 {\n", name)
	fmt.Fprintf(buf, "\tvar w uint64\n")

	for _, f := range bp.Fields {
		shift := f.BitOffset
		maskHex := fmt.Sprintf("0x%x", f.Mask)

		if f.IsBool {
			if shift == 0 {
				fmt.Fprintf(buf, "\tif s.%s {\n\t\tw |= 1\n\t}\n", f.GoName)
			} else {
				fmt.Fprintf(buf, "\tif s.%s {\n\t\tw |= (1 << %d)\n\t}\n", f.GoName, shift)
			}
		} else {
			valExpr := fmt.Sprintf("uint64(s.%s)", f.GoName)
			if shift == 0 {
				fmt.Fprintf(buf, "\tw |= (%s & %s)\n", valExpr, maskHex)
			} else {
				fmt.Fprintf(buf, "\tw |= (%s & %s) << %d\n", valExpr, maskHex, shift)
			}
		}
	}

	fmt.Fprintf(buf, "\treturn w\n")
	fmt.Fprintf(buf, "}\n\n")

	// UnpackUint64
	fmt.Fprintf(buf, "// UnpackUint64 extracts all bitfields directly from a raw 64-bit integer register value.\n")
	fmt.Fprintf(buf, "func (s *%s) UnpackUint64(w uint64) {\n", name)

	for _, f := range bp.Fields {
		shift := f.BitOffset
		maskHex := fmt.Sprintf("0x%x", f.Mask)
		typeName := f.Type.Name

		switch {
		case f.IsBool:
			if shift == 0 {
				fmt.Fprintf(buf, "\ts.%s = (w & 1) != 0\n", f.GoName)
			} else {
				fmt.Fprintf(buf, "\ts.%s = ((w >> %d) & 1) != 0\n", f.GoName, shift)
			}

		case f.IsSigned:
			if shift == 0 {
				fmt.Fprintf(buf, "\tv_%s := %s(w & %s)\n", f.GoName, typeName, maskHex)
			} else {
				fmt.Fprintf(buf, "\tv_%s := %s((w >> %d) & %s)\n", f.GoName, typeName, shift, maskHex)
			}

			nativeBitWidth := defaultTypeBitWidth(typeName)
			if f.BitWidth < nativeBitWidth {
				signBitHex := fmt.Sprintf("0x%x", uint64(1)<<(f.BitWidth-1))
				extMaskHex := fmt.Sprintf("^%s(0x%x)", typeName, f.Mask)
				fmt.Fprintf(
					buf,
					"\tif (v_%s & %s) != 0 {\n\t\tv_%s |= %s\n\t}\n",
					f.GoName,
					signBitHex,
					f.GoName,
					extMaskHex,
				)
			}

			fmt.Fprintf(buf, "\ts.%s = v_%s\n", f.GoName, f.GoName)

		default:
			if shift == 0 {
				fmt.Fprintf(buf, "\ts.%s = %s(w & %s)\n", f.GoName, typeName, maskHex)
			} else {
				fmt.Fprintf(buf, "\ts.%s = %s((w >> %d) & %s)\n", f.GoName, typeName, shift, maskHex)
			}
		}
	}

	fmt.Fprintf(buf, "}\n\n")

	// PackTo
	fmt.Fprintf(
		buf,
		"// PackTo writes the binary bit-packed representation of s directly into dst[:%sByteSize].\n",
		name,
	)
	fmt.Fprintf(buf, "func (s *%s) PackTo(dst []byte) int {\n", name)
	fmt.Fprintf(buf, "\t_ = dst[%sByteSize-1]\n", name)
	fmt.Fprintf(buf, "\tw := s.PackUint64()\n")

	switch totalBytes {
	case 8:
		fmt.Fprintf(buf, "\t%s.PutUint64(dst[:8], w)\n", endianOrder)
	case 4:
		fmt.Fprintf(buf, "\t%s.PutUint32(dst[:4], uint32(w))\n", endianOrder)
	case 2:
		fmt.Fprintf(buf, "\t%s.PutUint16(dst[:2], uint16(w))\n", endianOrder)
	case 1:
		fmt.Fprintf(buf, "\tdst[0] = byte(w)\n")
	default:
		fmt.Fprintf(buf, "\tvar tmp [8]byte\n")

		if endianOrder == "binary.BigEndian" {
			fmt.Fprintf(buf, "\t%s.PutUint64(tmp[:], w<<(8*(8-%sByteSize)))\n", endianOrder, name)
		} else {
			fmt.Fprintf(buf, "\t%s.PutUint64(tmp[:], w)\n", endianOrder)
		}

		fmt.Fprintf(buf, "\tcopy(dst[:%sByteSize], tmp[:%sByteSize])\n", name, name)
	}

	fmt.Fprintf(buf, "\treturn %sByteSize\n", name)
	fmt.Fprintf(buf, "}\n\n")

	// UnpackFrom
	fmt.Fprintf(buf, "// UnpackFrom decodes s directly from src without heap allocations or error wrapping.\n")
	fmt.Fprintf(buf, "func (s *%s) UnpackFrom(src []byte) int {\n", name)
	fmt.Fprintf(buf, "\t_ = src[%sByteSize-1]\n", name)

	switch totalBytes {
	case 8:
		fmt.Fprintf(buf, "\tw := %s.Uint64(src[:8])\n", endianOrder)
	case 4:
		fmt.Fprintf(buf, "\tw := uint64(%s.Uint32(src[:4]))\n", endianOrder)
	case 2:
		fmt.Fprintf(buf, "\tw := uint64(%s.Uint16(src[:2]))\n", endianOrder)
	case 1:
		fmt.Fprintf(buf, "\tw := uint64(src[0])\n")
	default:
		fmt.Fprintf(buf, "\tvar tmp [8]byte\n")

		if endianOrder == "binary.BigEndian" {
			fmt.Fprintf(buf, "\tcopy(tmp[8-%sByteSize:], src[:%sByteSize])\n", name, name)
		} else {
			fmt.Fprintf(buf, "\tcopy(tmp[:%sByteSize], src[:%sByteSize])\n", name, name)
		}

		fmt.Fprintf(buf, "\tw := %s.Uint64(tmp[:])\n", endianOrder)
	}

	fmt.Fprintf(buf, "\ts.UnpackUint64(w)\n")
	fmt.Fprintf(buf, "\treturn %sByteSize\n", name)
	fmt.Fprintf(buf, "}\n\n")
}

func emitBitpackMultiWord(buf *bytes.Buffer, bp *ir.BitpackIR, endianOrder string, hasBools bool) {
	name := bp.Name
	totalBytes := bp.TotalBytes
	numWords := (bp.TotalBits + 63) / 64

	// PackTo
	fmt.Fprintf(
		buf,
		"// PackTo writes the binary bit-packed representation of s directly into dst[:%sByteSize].\n",
		name,
	)
	fmt.Fprintf(buf, "func (s *%s) PackTo(dst []byte) int {\n", name)
	fmt.Fprintf(buf, "\t_ = dst[%sByteSize-1]\n", name)

	for w := 0; w < numWords; w++ {
		fmt.Fprintf(buf, "\tvar w%d uint64\n", w)
	}

	for _, f := range bp.Fields {
		wordIdx := f.BitOffset / 64
		bitInWord := f.BitOffset % 64
		maskHex := fmt.Sprintf("0x%x", f.Mask)

		switch {
		case f.IsBool:
			if bitInWord == 0 {
				fmt.Fprintf(buf, "\tif s.%s {\n\t\tw%d |= 1\n\t}\n", f.GoName, wordIdx)
			} else {
				fmt.Fprintf(buf, "\tif s.%s {\n\t\tw%d |= (1 << %d)\n\t}\n", f.GoName, wordIdx, bitInWord)
			}

		case bitInWord+f.BitWidth <= 64:
			valExpr := fmt.Sprintf("uint64(s.%s)", f.GoName)
			if bitInWord == 0 {
				fmt.Fprintf(buf, "\tw%d |= (%s & %s)\n", wordIdx, valExpr, maskHex)
			} else {
				fmt.Fprintf(buf, "\tw%d |= (%s & %s) << %d\n", wordIdx, valExpr, maskHex, bitInWord)
			}

		default:
			// Field crosses 64-bit word boundary
			bitsInFirst := 64 - bitInWord
			bitsInSecond := f.BitWidth - bitsInFirst
			mask1Hex := bitMaskHex(bitsInFirst)
			mask2Hex := bitMaskHex(bitsInSecond)
			valExpr := fmt.Sprintf("uint64(s.%s)", f.GoName)

			fmt.Fprintf(buf, "\tw%d |= (%s & %s) << %d\n", wordIdx, valExpr, mask1Hex, bitInWord)
			fmt.Fprintf(buf, "\tw%d |= (%s >> %d) & %s\n", wordIdx+1, valExpr, bitsInFirst, mask2Hex)
		}
	}

	// Write words out
	for w := 0; w < numWords; w++ {
		byteOffset := w * 8
		if byteOffset+8 <= totalBytes {
			fmt.Fprintf(buf, "\t%s.PutUint64(dst[%d:%d], w%d)\n", endianOrder, byteOffset, byteOffset+8, w)
		} else {
			rem := totalBytes - byteOffset

			fmt.Fprintf(buf, "\tvar tmp%d [8]byte\n", w)

			if endianOrder == "binary.BigEndian" {
				fmt.Fprintf(buf, "\t%s.PutUint64(tmp%d[:], w%d<<(8*(8-%d)))\n", endianOrder, w, w, rem)
			} else {
				fmt.Fprintf(buf, "\t%s.PutUint64(tmp%d[:], w%d)\n", endianOrder, w, w)
			}

			fmt.Fprintf(buf, "\tcopy(dst[%d:%d], tmp%d[:%d])\n", byteOffset, totalBytes, w, rem)
		}
	}

	fmt.Fprintf(buf, "\treturn %sByteSize\n", name)
	fmt.Fprintf(buf, "}\n\n")

	// UnpackFrom
	fmt.Fprintf(buf, "// UnpackFrom decodes s directly from src without heap allocations or error wrapping.\n")
	fmt.Fprintf(buf, "func (s *%s) UnpackFrom(src []byte) int {\n", name)
	fmt.Fprintf(buf, "\t_ = src[%sByteSize-1]\n", name)

	for w := 0; w < numWords; w++ {
		byteOffset := w * 8
		if byteOffset+8 <= totalBytes {
			fmt.Fprintf(buf, "\tw%d := %s.Uint64(src[%d:%d])\n", w, endianOrder, byteOffset, byteOffset+8)
		} else {
			rem := totalBytes - byteOffset

			fmt.Fprintf(buf, "\tvar tmp%d [8]byte\n", w)

			if endianOrder == "binary.BigEndian" {
				fmt.Fprintf(buf, "\tcopy(tmp%d[8-%d:], src[%d:%d])\n", w, rem, byteOffset, totalBytes)
			} else {
				fmt.Fprintf(buf, "\tcopy(tmp%d[:%d], src[%d:%d])\n", w, rem, byteOffset, totalBytes)
			}

			fmt.Fprintf(buf, "\tw%d := %s.Uint64(tmp%d[:])\n", w, endianOrder, w)
		}
	}

	for _, f := range bp.Fields {
		wordIdx := f.BitOffset / 64
		bitInWord := f.BitOffset % 64
		maskHex := fmt.Sprintf("0x%x", f.Mask)
		typeName := f.Type.Name

		switch {
		case f.IsBool:
			if bitInWord == 0 {
				fmt.Fprintf(buf, "\ts.%s = (w%d & 1) != 0\n", f.GoName, wordIdx)
			} else {
				fmt.Fprintf(buf, "\ts.%s = ((w%d >> %d) & 1) != 0\n", f.GoName, wordIdx, bitInWord)
			}

		case bitInWord+f.BitWidth <= 64 && f.IsSigned:
			if bitInWord == 0 {
				fmt.Fprintf(buf, "\tv_%s := %s(w%d & %s)\n", f.GoName, typeName, wordIdx, maskHex)
			} else {
				fmt.Fprintf(
					buf,
					"\tv_%s := %s((w%d >> %d) & %s)\n",
					f.GoName,
					typeName,
					wordIdx,
					bitInWord,
					maskHex,
				)
			}

			nativeBitWidth := defaultTypeBitWidth(typeName)
			if f.BitWidth < nativeBitWidth {
				signBitHex := fmt.Sprintf("0x%x", uint64(1)<<(f.BitWidth-1))
				extMaskHex := fmt.Sprintf("^%s(0x%x)", typeName, f.Mask)
				fmt.Fprintf(
					buf,
					"\tif (v_%s & %s) != 0 {\n\t\tv_%s |= %s\n\t}\n",
					f.GoName,
					signBitHex,
					f.GoName,
					extMaskHex,
				)
			}

			fmt.Fprintf(buf, "\ts.%s = v_%s\n", f.GoName, f.GoName)

		case bitInWord+f.BitWidth <= 64:
			if bitInWord == 0 {
				fmt.Fprintf(buf, "\ts.%s = %s(w%d & %s)\n", f.GoName, typeName, wordIdx, maskHex)
			} else {
				fmt.Fprintf(buf, "\ts.%s = %s((w%d >> %d) & %s)\n", f.GoName, typeName, wordIdx, bitInWord, maskHex)
			}

		default:
			// Field crosses 64-bit word boundary
			bitsInFirst := 64 - bitInWord
			bitsInSecond := f.BitWidth - bitsInFirst
			mask1Hex := bitMaskHex(bitsInFirst)
			mask2Hex := bitMaskHex(bitsInSecond)

			fmt.Fprintf(
				buf,
				"\tv_%s := ((w%d >> %d) & %s) | ((w%d & %s) << %d)\n",
				f.GoName,
				wordIdx,
				bitInWord,
				mask1Hex,
				wordIdx+1,
				mask2Hex,
				bitsInFirst,
			)
			fmt.Fprintf(buf, "\ts.%s = %s(v_%s)\n", f.GoName, typeName, f.GoName)
		}
	}

	fmt.Fprintf(buf, "\treturn %sByteSize\n", name)
	fmt.Fprintf(buf, "}\n\n")
}

func emitBitpackBatchSlice(buf *bytes.Buffer, bp *ir.BitpackIR) {
	name := bp.Name

	// Pack<Name>Slice
	fmt.Fprintf(
		buf,
		"// Pack%sSlice serializes a slice of %s into dst with loop-unrolled register pipelines.\n",
		name,
		name,
	)
	fmt.Fprintf(buf, "func Pack%sSlice(dst []byte, items []%s) []byte {\n", name, name)
	fmt.Fprintf(buf, "\tneeded := len(items) * %sByteSize\n", name)
	fmt.Fprintf(buf, "\tif cap(dst)-len(dst) < needed {\n")
	fmt.Fprintf(buf, "\t\tnewDst := make([]byte, len(dst)+needed)\n")
	fmt.Fprintf(buf, "\t\tcopy(newDst, dst)\n")
	fmt.Fprintf(buf, "\t\tdst = newDst\n")
	fmt.Fprintf(buf, "\t} else {\n")
	fmt.Fprintf(buf, "\t\tdst = dst[:len(dst)+needed]\n")
	fmt.Fprintf(buf, "\t}\n\n")
	fmt.Fprintf(buf, "\tout := dst[len(dst)-needed:]\n")
	fmt.Fprintf(buf, "\tn := len(items)\n")
	fmt.Fprintf(buf, "\ti := 0\n\n")
	fmt.Fprintf(buf, "\t// 4x loop unrolling for instruction-level parallelism (ILP)\n")
	fmt.Fprintf(buf, "\tfor ; i+3 < n; i += 4 {\n")
	fmt.Fprintf(buf, "\t\titems[i].PackTo(out[i*%sByteSize:])\n", name)
	fmt.Fprintf(buf, "\t\titems[i+1].PackTo(out[(i+1)*%sByteSize:])\n", name)
	fmt.Fprintf(buf, "\t\titems[i+2].PackTo(out[(i+2)*%sByteSize:])\n", name)
	fmt.Fprintf(buf, "\t\titems[i+3].PackTo(out[(i+3)*%sByteSize:])\n", name)
	fmt.Fprintf(buf, "\t}\n\n")
	fmt.Fprintf(buf, "\tfor ; i < n; i++ {\n")
	fmt.Fprintf(buf, "\t\titems[i].PackTo(out[i*%sByteSize:])\n", name)
	fmt.Fprintf(buf, "\t}\n\n")
	fmt.Fprintf(buf, "\treturn dst\n")
	fmt.Fprintf(buf, "}\n\n")

	// Unpack<Name>Slice
	fmt.Fprintf(buf, "// Unpack%sSlice decodes a continuous stream of %s items from src into dst.\n", name, name)
	fmt.Fprintf(buf, "func Unpack%sSlice(dst []%s, src []byte) ([]%s, error) {\n", name, name, name)
	fmt.Fprintf(buf, "\tcount := len(src) / %sByteSize\n", name)
	fmt.Fprintf(buf, "\tif count == 0 {\n")
	fmt.Fprintf(buf, "\t\treturn dst, nil\n")
	fmt.Fprintf(buf, "\t}\n\n")
	fmt.Fprintf(buf, "\tif cap(dst)-len(dst) < count {\n")
	fmt.Fprintf(buf, "\t\tnewDst := make([]%s, len(dst), len(dst)+count)\n", name)
	fmt.Fprintf(buf, "\t\tcopy(newDst, dst)\n")
	fmt.Fprintf(buf, "\t\tdst = newDst\n")
	fmt.Fprintf(buf, "\t}\n\n")
	fmt.Fprintf(buf, "\tstart := len(dst)\n")
	fmt.Fprintf(buf, "\tdst = dst[:start+count]\n\n")
	fmt.Fprintf(buf, "\ti := 0\n")
	fmt.Fprintf(buf, "\t// 4x loop unrolling\n")
	fmt.Fprintf(buf, "\tfor ; i+3 < count; i += 4 {\n")
	fmt.Fprintf(buf, "\t\tdst[start+i].UnpackFrom(src[i*%sByteSize:])\n", name)
	fmt.Fprintf(buf, "\t\tdst[start+i+1].UnpackFrom(src[(i+1)*%sByteSize:])\n", name)
	fmt.Fprintf(buf, "\t\tdst[start+i+2].UnpackFrom(src[(i+2)*%sByteSize:])\n", name)
	fmt.Fprintf(buf, "\t\tdst[start+i+3].UnpackFrom(src[(i+3)*%sByteSize:])\n", name)
	fmt.Fprintf(buf, "\t}\n\n")
	fmt.Fprintf(buf, "\tfor ; i < count; i++ {\n")
	fmt.Fprintf(buf, "\t\tdst[start+i].UnpackFrom(src[i*%sByteSize:])\n", name)
	fmt.Fprintf(buf, "\t}\n\n")
	fmt.Fprintf(buf, "\treturn dst, nil\n")
	fmt.Fprintf(buf, "}\n\n")
}

func bitMaskHex(bitWidth int) string {
	if bitWidth >= 64 {
		return "0xffffffffffffffff"
	}

	if bitWidth <= 0 {
		return "0x0"
	}

	return fmt.Sprintf("0x%x", (uint64(1)<<bitWidth)-1)
}

func defaultTypeBitWidth(typeName string) int {
	switch typeName {
	case "bool":
		return 1
	case "uint8", "byte", "int8":
		return 8
	case "uint16", "int16":
		return 16
	case "uint32", "int32":
		return 32
	case "uint64", "int64", "uint", "int", "uintptr":
		return 64
	default:
		return 8
	}
}
