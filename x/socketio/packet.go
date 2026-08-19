// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socketio

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/realtime/ws"
)

const (
	eioOpen    = '0'
	eioClose   = '1'
	eioPing    = '2'
	eioPong    = '3'
	eioMessage = '4'
	eioUpgrade = '5'
	eioNoop    = '6'
	eioBinary  = 'b'
)

const (
	sioConnect      byte = '0'
	sioDisconnect   byte = '1'
	sioEvent        byte = '2'
	sioAck          byte = '3'
	sioConnectError byte = '4'
	sioBinaryEvent  byte = '5'
	sioBinaryAck    byte = '6'
)

type sioPacket struct {
	Type        byte
	Namespace   string
	ID          *int64
	Attachments int
	Data        json.RawMessage
}

type binaryReconstructor struct {
	attachments int
	buffers     [][]byte
	packet      *sioPacket
}

const (
	maxBinaryAttachments = 64
	maxBinaryBufferSize  = 32 * 1024 * 1024
	maxEIOPacketSize     = 8 * 1024 * 1024
)

func newBinaryReconstructor(attachments int, pkt *sioPacket) *binaryReconstructor {
	if attachments > maxBinaryAttachments {
		attachments = maxBinaryAttachments
	}

	return &binaryReconstructor{
		attachments: attachments,
		packet:      pkt,
	}
}

func (br *binaryReconstructor) addBuffer(data []byte) bool {
	br.buffers = append(br.buffers, data)

	totalSize := 0
	for _, buf := range br.buffers {
		totalSize += len(buf)
		if totalSize > maxBinaryBufferSize {
			return true
		}
	}

	return len(br.buffers) >= br.attachments
}

func (br *binaryReconstructor) reconstruct() (*sioPacket, error) {
	if len(br.buffers) != br.attachments {
		return nil, fmt.Errorf("aoni/socketio: expected %d attachments, got %d", br.attachments, len(br.buffers))
	}

	pkt := *br.packet

	var rawArgs []json.RawMessage
	if err := json.Unmarshal(pkt.Data, &rawArgs); err != nil {
		return nil, fmt.Errorf("aoni/socketio: unmarshal binary packet: %w", err)
	}

	data := make([]any, len(rawArgs))
	for i, raw := range rawArgs {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("aoni/socketio: unmarshal binary arg: %w", err)
		}

		data[i] = reconstructBinary(v, br.buffers)
	}

	pkt.Data, _ = json.Marshal(data)
	pkt.Type = sioEvent

	return &pkt, nil
}

func readSingleEIOPacket(conn ws.Conn) (byte, []byte, error) {
	msgType, data, err := conn.ReadMessage()
	if err != nil {
		return 0, nil, err
	}

	if len(data) == 0 {
		return 0, nil, io.EOF
	}

	if len(data) > maxEIOPacketSize {
		return 0, nil, errors.New("aoni/socketio: packet too large")
	}

	if msgType == ws.FrameBinary {
		return eioBinary, data, nil
	}

	return data[0], data[1:], nil
}

func encodeSIOPacket(pkt sioPacket) []byte {
	var sb strings.Builder

	sb.WriteByte(pkt.Type)

	if pkt.Type == sioBinaryEvent || pkt.Type == sioBinaryAck {
		sb.WriteString(strconv.Itoa(pkt.Attachments))
		sb.WriteByte('-')
	}

	if pkt.Namespace != "" && pkt.Namespace != "/" {
		sb.WriteString(pkt.Namespace)
		sb.WriteByte(',')
	}

	if pkt.ID != nil {
		sb.WriteString(strconv.FormatInt(*pkt.ID, 10))
	}

	if pkt.Data != nil {
		sb.Write(pkt.Data)
	}

	return []byte(sb.String())
}

func decodeSIOPacket(data []byte) (*sioPacket, error) {
	if len(data) == 0 {
		return nil, ErrEmptyPacket
	}

	pkt := &sioPacket{Type: data[0]}
	offset := 1

	if pkt.Type == sioBinaryEvent || pkt.Type == sioBinaryAck {
		attachments, err := parseAttachments(data, &offset)
		if err != nil {
			return nil, err
		}

		pkt.Attachments = attachments
	}

	pkt.Namespace = parseNamespace(data, &offset)
	pkt.ID = parseAckID(data, &offset)

	if offset < len(data) {
		pkt.Data = make(json.RawMessage, len(data)-offset)
		copy(pkt.Data, data[offset:])
	}

	return pkt, nil
}

func parseAttachments(data []byte, offset *int) (int, error) {
	start := *offset

	i := start
	for i < len(data) && data[i] != '-' {
		i++
	}

	attachments, err := strconv.Atoi(string(data[start:i]))
	if err != nil || attachments < 0 || attachments > maxBinaryAttachments {
		return 0, fmt.Errorf("aoni/socketio: invalid attachment count: %q", string(data[start:i]))
	}

	if i < len(data) {
		i++
	}

	*offset = i

	return attachments, nil
}

func parseNamespace(data []byte, offset *int) string {
	i := *offset
	if i >= len(data) || data[i] != '/' {
		return "/"
	}

	start := i
	for i < len(data) && data[i] != ',' {
		i++
	}

	nsp := string(data[start:i])
	if i < len(data) {
		i++
	}

	*offset = i

	return nsp
}

func parseAckID(data []byte, offset *int) *int64 {
	i := *offset
	if i >= len(data) || data[i] < '0' || data[i] > '9' {
		return nil
	}

	start := i
	for i < len(data) && data[i] >= '0' && data[i] <= '9' {
		i++
	}

	id, _ := strconv.ParseInt(string(data[start:i]), 10, 64)
	*offset = i

	return &id
}

func hasBinary(obj any) bool {
	switch v := obj.(type) {
	case []byte:
		return true
	case []any:
		if slices.ContainsFunc(v, hasBinary) {
			return true
		}
	case map[string]any:
		for _, val := range v {
			if hasBinary(val) {
				return true
			}
		}

	case map[string]json.RawMessage:
		for _, val := range v {
			if hasBinaryRaw(val) {
				return true
			}
		}
	}

	return false
}

func hasBinaryRaw(raw json.RawMessage) bool {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}

	return hasBinary(v)
}

func deconstructBinary(data any) (any, [][]byte) {
	var buffers [][]byte

	result := deconstructBinaryWithOffset(data, &buffers)

	return result, buffers
}

func deconstructBinaryWithOffset(data any, buffers *[][]byte) any {
	switch v := data.(type) {
	case []byte:
		idx := len(*buffers)
		*buffers = append(*buffers, v)

		return map[string]any{"_placeholder": true, "num": idx}

	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = deconstructBinaryWithOffset(item, buffers)
		}

		return result

	case map[string]any:
		result := make(map[string]any, len(v))
		for key, val := range v {
			result[key] = deconstructBinaryWithOffset(val, buffers)
		}

		return result
	}

	return data
}

func reconstructBinary(data any, buffers [][]byte) any {
	switch v := data.(type) {
	case map[string]any:
		if isPlaceholder(v) {
			return resolvePlaceholderBuffer(v, buffers)
		}

		result := make(map[string]any, len(v))
		for key, val := range v {
			result[key] = reconstructBinary(val, buffers)
		}

		return result

	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = reconstructBinary(item, buffers)
		}

		return result
	}

	return data
}

func isPlaceholder(m map[string]any) bool {
	ph, ok := m["_placeholder"]
	return ok && ph == true
}

func resolvePlaceholderBuffer(m map[string]any, buffers [][]byte) any {
	idx := -1
	switch n := m["num"].(type) {
	case float64:
		idx = int(n)
	case int:
		idx = n
	case json.Number:
		val, _ := n.Int64()
		idx = int(val)
	}

	if idx >= 0 && idx < len(buffers) {
		return buffers[idx]
	}

	return m
}
