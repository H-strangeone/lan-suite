/*
  CONCEPT: State Management with Zustand
  ────────────────────────────────────────
  Zustand is a minimal state management library.
  Think of it as a global useState that ANY component can access.

  WHY NOT just use React Context?
  Context re-renders ALL consumers when ANY value changes.
  Zustand only re-renders components that use the specific slice
  of state that changed. Much more performant.

  PATTERN: The store is a function that receives get/set.
  - set(partial) merges partial state into current state
  - get() reads current state
  Components use: const peers = useNodeStore(state => state.peers)
  This component only re-renders when `peers` changes, nothing else.

  INSTALL: npm install zustand
*/

import { create } from 'zustand'

/*
  Connection states — explicit string union is cleaner than booleans.
  'disconnected' → 'connecting' → 'connected' → 'error'
  Never use magic strings inline — define them as constants.
*/
export const CONNECTION_STATE = {
  DISCONNECTED: 'disconnected',
  CONNECTING:   'connecting',
  CONNECTED:    'connected',
  ERROR:        'error',
}

export const useNodeStore = create((set, get) => ({
  // ── Node Identity ──
  // This node's unique ID — in Phase 2 derived from a keypair
  nodeId: generateNodeId(),

  // ── WebSocket / Signaling ──
  wsConnection: null,                        // the actual WebSocket object
  connectionState: CONNECTION_STATE.DISCONNECTED,

  // ── Peers ──
  // Map of peerId → peer object
  // Using an object (not array) for O(1) lookup by ID
  peers: {},
  /*
    Peer shape:
    {
      id: 'AA:BB:CC:DD:EE:FF',
      displayName: 'Alice-MacBook',
      services: ['chat', 'video', 'drive'],
      lastSeen: Date,
      online: true,
    }
  */

  // ── Actions ──
  // Actions are just functions inside the store.
  // They call set() to update state.

  setConnectionState: (state) => set({ connectionState: state }),

  setWsConnection: (ws) => set({ wsConnection: ws }),

  addPeer: (peer) => set(state => ({
    peers: { ...state.peers, [peer.id]: peer }
  })),

  removePeer: (peerId) => set(state => {
    const next = { ...state.peers }
    delete next[peerId]
    return { peers: next }
  }),

  updatePeer: (peerId, updates) => set(state => ({
    peers: {
      ...state.peers,
      [peerId]: { ...state.peers[peerId], ...updates }
    }
  })),

  // Convenience getter — returns array from peers map
  getPeerList: () => Object.values(get().peers),
  getOnlinePeers: () => Object.values(get().peers).filter(p => p.online),
}))

function generateNodeId() {
  return Array.from(crypto.getRandomValues(new Uint8Array(6)))
    .map(b => b.toString(16).padStart(2, '0').toUpperCase())
    .join(':')
}

/*
  USAGE IN COMPONENTS:
  ─────────────────────
  import { useNodeStore } from '../store/nodeStore'

  function SomeComponent() {
    // Only re-renders when peers changes
    const peers = useNodeStore(state => state.peers)
    const addPeer = useNodeStore(state => state.addPeer)

    // Or grab multiple values (still efficient with shallow compare):
    const { nodeId, connectionState } = useNodeStore(state => ({
      nodeId: state.nodeId,
      connectionState: state.connectionState,
    }))
  }
*/