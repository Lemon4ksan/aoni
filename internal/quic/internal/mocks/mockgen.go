// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build gomock || generate

package mocks

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package mocks -destination short_header_sealer.go github.com/lemon4ksan/aoni/internal/quic/internal/handshake ShortHeaderSealer"
//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package mocks -destination short_header_opener.go github.com/lemon4ksan/aoni/internal/quic/internal/handshake ShortHeaderOpener"
//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package mocks -destination long_header_opener.go github.com/lemon4ksan/aoni/internal/quic/internal/handshake LongHeaderOpener"
//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package mocks -destination crypto_setup.go github.com/lemon4ksan/aoni/internal/quic/internal/handshake CryptoSetup"
//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package mocks -destination congestion.go github.com/lemon4ksan/aoni/internal/quic/internal/congestion SendAlgorithmWithDebugInfos"
//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package mockackhandler -destination ackhandler/sent_packet_handler.go github.com/lemon4ksan/aoni/internal/quic/internal/ackhandler SentPacketHandler"
