// Package transport (quic.go) implements QUIC-based file transfer.
// Large files are never sent over WebSocket — they go through QUIC.
//
// STATUS: Stub — implemented in Block 7 (file transfer phase)
//
// WHY QUIC OVER TCP FOR FILES?
//
//   TCP problem — Head-of-line blocking:
//   If you're transferring 3 chunks simultaneously over one TCP connection:
//   Chunk 1 ──────────────→ arrives
//   Chunk 2 ──────X (lost) → TCP retransmits
//   Chunk 3 ──────────────→ BLOCKED waiting for Chunk 2
//
//   QUIC solution — independent streams:
//   Each chunk is a separate QUIC stream.
//   A lost packet in stream 2 doesn't block streams 1 and 3.
//   3x throughput on lossy networks.
//
//   Also: QUIC has built-in TLS 1.3. No separate TLS handshake.
//   Connection setup is 0-RTT for known peers (faster reconnection).
//
// DESIGN:
//
//   Server side (each node runs this):
//   - Listen on UDP :4242 for QUIC connections
//   - Each incoming stream: read Interest packet → serve chunk from CS
//
//   Client side (requesting a file):
//   - Connect to peer's QUIC address
//   - Open one stream per chunk request
//   - Send Interest, receive Data
//   - All streams run concurrently → parallel chunk download
//
//   Integration with CCN:
//   QUIC is just another "face" in the CCN router.
//   Interest arrives via QUIC → router.ReceiveInterest(interest, peerNodeID)
//   Data to send via QUIC → send callback calls quic.SendToPeer(peer, data)
package transport

// QUICServer handles incoming QUIC connections for file transfer.
// Stub — implemented in Block 7.
type QUICServer struct{}

// NewQUICServer creates a QUICServer.
func NewQUICServer() *QUICServer { return &QUICServer{} }

// Start begins listening for QUIC connections on the configured port.
func (q *QUICServer) Start(addr string) error {
	// TODO Block 7:
	// listener, err := quic.ListenAddr(addr, generateTLSConfig(), nil)
	// go q.acceptLoop(listener)
	return nil
}

// Stop shuts down the QUIC server.
func (q *QUICServer) Stop() {}