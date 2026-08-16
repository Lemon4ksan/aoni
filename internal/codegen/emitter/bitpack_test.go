// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func TestBitpack_EmissionAndExecution(t *testing.T) {
	src := `package packet

// @aoni:bitpack endian=little
type PacketHeader struct {
	OpCode uint8  ` + "`" + `bits:"3"` + "`" + `
	IsAck  bool   ` + "`" + `bits:"1"` + "`" + `
	Length uint32 ` + "`" + `bits:"28"` + "`" + `
	JobID  uint32 ` + "`" + `bits:"32"` + "`" + `
}

// @aoni:bitpack endian=big
type BigHeader struct {
	Flags  uint16 ` + "`" + `bits:"12"` + "`" + `
	Type   uint8  ` + "`" + `bits:"4"` + "`" + `
	Offset uint32 ` + "`" + `bits:"32"` + "`" + `
}

// @aoni:bitpack endian=little
type ExtendedPacket struct {
	Version   uint8  ` + "`" + `bits:"4"` + "`" + `
	Flags     uint16 ` + "`" + `bits:"12"` + "`" + `
	DeltaTime int16  ` + "`" + `bits:"12"` + "`" + `
	StreamID  uint64 ` + "`" + `bits:"50"` + "`" + `
	Sequence  uint64 ` + "`" + `bits:"50"` + "`" + `
}
`
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module packet\n\ngo 1.25.4\n"), 0o600)
	require.NoError(t, err)

	apiFile := filepath.Join(tmpDir, "packet.go")
	err = os.WriteFile(apiFile, []byte(src), 0o600)
	require.NoError(t, err)

	p := parser.NewParser()
	root, err := p.ParseFile(apiFile)
	require.NoError(t, err)
	require.Len(t, root.Bitpacks, 3)

	// Check ExtendedPacket (128 bits = 16 bytes)
	ext := root.Bitpacks[2]
	require.Equal(t, "ExtendedPacket", ext.Name)
	require.Equal(t, 128, ext.TotalBits)
	require.Equal(t, 16, ext.TotalBytes)
	require.True(t, ext.Fields[2].IsSigned)

	// Emit Go code
	em := emitter.NewEmitter()
	genBytes, err := em.Emit(root)
	require.NoError(t, err)

	genFile := filepath.Join(tmpDir, "packet.gen.go")
	err = os.WriteFile(genFile, genBytes, 0o600)
	require.NoError(t, err)

	// Write verification test in the temporary package and run go test
	testSrc := `package packet

import (
	"testing"
)

func TestPacketHeader_Roundtrip(t *testing.T) {
	orig := PacketHeader{
		OpCode: 5,
		IsAck:  true,
		Length: 1048576,
		JobID:  987654321,
	}

	// 1. Pack / Unpack
	packed, err := orig.Pack(nil)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}
	if len(packed) != PacketHeaderByteSize {
		t.Fatalf("expected length %d, got %d", PacketHeaderByteSize, len(packed))
	}

	var decoded PacketHeader
	if err := decoded.Unpack(packed); err != nil {
		t.Fatalf("Unpack failed: %v", err)
	}

	if decoded != orig {
		t.Fatalf("mismatch: expected %+v, got %+v", orig, decoded)
	}

	// 2. Register direct PackUint64 / UnpackUint64
	w := orig.PackUint64()
	var decodedReg PacketHeader
	decodedReg.UnpackUint64(w)
	if decodedReg != orig {
		t.Fatalf("register mismatch: expected %+v, got %+v", orig, decodedReg)
	}

	// 3. Batch Slice Pack/Unpack
	items := []PacketHeader{orig, orig, orig, orig, orig}
	batchBuf := PackPacketHeaderSlice(nil, items)
	if len(batchBuf) != len(items)*PacketHeaderByteSize {
		t.Fatalf("batch len mismatch: got %d", len(batchBuf))
	}

	unpackedItems, err := UnpackPacketHeaderSlice(nil, batchBuf)
	if err != nil {
		t.Fatalf("batch unpack failed: %v", err)
	}
	if len(unpackedItems) != len(items) {
		t.Fatalf("batch count mismatch: got %d", len(unpackedItems))
	}
	for i, item := range unpackedItems {
		if item != orig {
			t.Fatalf("batch item %d mismatch: got %+v", i, item)
		}
	}
}

func TestBigHeader_Roundtrip(t *testing.T) {
	orig := BigHeader{
		Flags:  4095,
		Type:   15,
		Offset: 305419896,
	}

	packed, err := orig.Pack(nil)
	if err != nil {
		t.Fatalf("BigHeader Pack failed: %v", err)
	}

	var decoded BigHeader
	if err := decoded.Unpack(packed); err != nil {
		t.Fatalf("BigHeader Unpack failed: %v", err)
	}

	if decoded != orig {
		t.Fatalf("BigHeader mismatch: expected %+v, got %+v", orig, decoded)
	}
}

func TestExtendedPacket_Roundtrip(t *testing.T) {
	orig := ExtendedPacket{
		Version:   12,
		Flags:     2048,
		DeltaTime: -100, // Negative signed bitfield
		StreamID:  11223344556677,
		Sequence:  99887766554433,
	}

	packed, err := orig.Pack(nil)
	if err != nil {
		t.Fatalf("ExtendedPacket Pack failed: %v", err)
	}
	if len(packed) != ExtendedPacketByteSize {
		t.Fatalf("expected len %d, got %d", ExtendedPacketByteSize, len(packed))
	}

	var decoded ExtendedPacket
	if err := decoded.Unpack(packed); err != nil {
		t.Fatalf("ExtendedPacket Unpack failed: %v", err)
	}

	if decoded != orig {
		t.Fatalf("ExtendedPacket mismatch: expected %+v, got %+v", orig, decoded)
	}
}

func BenchmarkPacketHeader_PackUint64(b *testing.B) {
	hdr := PacketHeader{OpCode: 7, IsAck: true, Length: 500000, JobID: 123456}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = hdr.PackUint64()
	}
}

func BenchmarkPacketHeader_PackTo(b *testing.B) {
	hdr := PacketHeader{OpCode: 7, IsAck: true, Length: 500000, JobID: 123456}
	var buf [8]byte
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		hdr.PackTo(buf[:])
	}
}

func BenchmarkPacketHeader_BatchUnpack(b *testing.B) {
	items := make([]PacketHeader, 1000)
	for i := range items {
		items[i] = PacketHeader{OpCode: 5, IsAck: true, Length: uint32(i), JobID: uint32(i * 10)}
	}
	raw := PackPacketHeaderSlice(nil, items)
	dst := make([]PacketHeader, 1000)

	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = UnpackPacketHeaderSlice(dst[:0], raw)
	}
}
`
	runnerFile := filepath.Join(tmpDir, "packet_test.go")
	err = os.WriteFile(runnerFile, []byte(testSrc), 0o600)
	require.NoError(t, err)

	cmd := exec.Command("go", "test", "-v", "-bench=.", "-benchmem", ".")
	cmd.Dir = tmpDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go test failed:\n%s", string(out))
	require.Contains(t, string(out), "PASS")
	t.Logf("Benchmark Results:\n%s", string(out))
}
