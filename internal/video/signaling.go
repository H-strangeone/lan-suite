// Package video handles WebRTC signaling via CCN.
// The actual video/audio streams are peer-to-peer (browser↔browser).
// This package only handles the signaling coordination.
//
// STATUS: Stub — implemented in Block 6 (after chat)
//
// DESIGN:
//
//   WHY CCN for signaling instead of just the WebSocket hub?
//   The hub already relays offer/answer/ice — it works.
//   CCN adds: persistence, named packets, multi-party calls.
//
//   Signaling packet names:
//   /video/calls/<callID>/offer         SDP offer
//   /video/calls/<callID>/answer        SDP answer
//   /video/calls/<callID>/ice/<idx>     ICE candidates
//
//   FLOW (two-party call):
//   1. Caller generates callID = uuid
//   2. Caller sends Interest: /video/calls/<callID>/answer (waiting for callee)
//   3. Caller publishes Data: /video/calls/<callID>/offer {sdp:...}
//   4. Callee receives offer via CCN
//   5. Callee publishes Data: /video/calls/<callID>/answer {sdp:...}
//   6. Caller's Interest is satisfied by the answer
//   7. Both exchange ICE candidates via /video/calls/<callID>/ice/<n>
//   8. WebRTC direct connection established
//   9. CCN packets are done — media flows P2P outside this system
//
//   MULTI-PARTY CALLS:
//   Each pair of peers does its own offer/answer.
//   callID is shared, peerID disambiguates:
//   /video/calls/<callID>/offer/<fromNodeID>/<toNodeID>
package video

import "github.com/H-strangeone/lan-suite/internal/config"

// Signaler coordinates WebRTC signaling via CCN.
type Signaler struct {
	cfg *config.Config
}

// New creates a Signaler.
func New(cfg *config.Config) *Signaler {
	return &Signaler{cfg: cfg}
}