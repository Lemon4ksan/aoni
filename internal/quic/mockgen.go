// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build gomock || generate

package quic

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_send_conn_test.go github.com/lemon4ksan/aoni/internal/quic SendConn"
type SendConn = sendConn

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_raw_conn_test.go github.com/lemon4ksan/aoni/internal/quic RawConn"
type RawConn = rawConn

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_sender_test.go github.com/lemon4ksan/aoni/internal/quic Sender"
type Sender = sender

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_stream_sender_test.go github.com/lemon4ksan/aoni/internal/quic StreamSender"
type StreamSender = streamSender

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_stream_control_frame_getter_test.go github.com/lemon4ksan/aoni/internal/quic StreamControlFrameGetter"
type StreamControlFrameGetter = streamControlFrameGetter

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_stream_frame_getter_test.go github.com/lemon4ksan/aoni/internal/quic StreamFrameGetter"
type StreamFrameGetter = streamFrameGetter

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_frame_source_test.go github.com/lemon4ksan/aoni/internal/quic FrameSource"
type FrameSource = frameSource

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_ack_frame_source_test.go github.com/lemon4ksan/aoni/internal/quic AckFrameSource"
type AckFrameSource = ackFrameSource

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_sealing_manager_test.go github.com/lemon4ksan/aoni/internal/quic SealingManager"
type SealingManager = sealingManager

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_unpacker_test.go github.com/lemon4ksan/aoni/internal/quic Unpacker"
type Unpacker = unpacker

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_packer_test.go github.com/lemon4ksan/aoni/internal/quic Packer"
type Packer = packer

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_conn_runner_test.go github.com/lemon4ksan/aoni/internal/quic ConnRunner"
type ConnRunner = connRunner

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_packet_handler_test.go github.com/lemon4ksan/aoni/internal/quic PacketHandler"
type PacketHandler = packetHandler

//go:generate sh -c "go tool mockgen -typed -package quic -self_package github.com/lemon4ksan/aoni/internal/quic -self_package github.com/lemon4ksan/aoni/internal/quic -destination mock_packetconn_test.go net PacketConn"
