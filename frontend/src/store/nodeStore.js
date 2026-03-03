
import { create } from 'zustand'

export const CONNECTION_STATE = {
  DISCONNECTED: 'disconnected',
  CONNECTING:   'connecting',
  CONNECTED:    'connected',
  ERROR:        'error',
}

export const useNodeStore = create((set, get) => ({

  nodeId: generateNodeId(),

  wsConnection: null,                      
  connectionState: CONNECTION_STATE.DISCONNECTED,
  peers: {},

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

  getPeerList: () => Object.values(get().peers),
  getOnlinePeers: () => Object.values(get().peers).filter(p => p.online),
}))

function generateNodeId() {
  return Array.from(crypto.getRandomValues(new Uint8Array(6)))
    .map(b => b.toString(16).padStart(2, '0').toUpperCase())
    .join(':')
}
