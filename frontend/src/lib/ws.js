import { useNodeStore, CONNECTION_STATE } from '../store/nodeStore.js'


const WS_URL = import.meta.env.VITE_WS_URL || 'ws://localhost:8080/ws'

let socket = null
let reconnectTimer = null
let reconnectAttempts = 0
const MAX_RECONNECT_ATTEMPTS = 10
const RECONNECT_BASE_DELAY_MS = 1000

const handlers = new Map()
export const ws = {
  connect() {
    const { setConnectionState, setWsConnection, nodeId } = useNodeStore.getState()

    if (socket && socket.readyState === WebSocket.OPEN) return

    setConnectionState(CONNECTION_STATE.CONNECTING)
    console.log('[ws] connecting to', WS_URL)

    try {
      socket = new WebSocket(WS_URL)
    } catch (err) {
      console.error('[ws] failed to create WebSocket:', err)
      setConnectionState(CONNECTION_STATE.ERROR)
      return
    }

    socket.addEventListener('open', () => {
      console.log('[ws] connected')
      reconnectAttempts = 0
      setConnectionState(CONNECTION_STATE.CONNECTED)
      setWsConnection(socket)
      ws.send({ type: 'hello', nodeId, services: ['chat', 'video', 'drive'] })
    })

    socket.addEventListener('message', (event) => {
      let msg
      try {
        msg = JSON.parse(event.data)
      } catch {
        console.warn('[ws] received non-JSON message:', event.data)
        return
      }

      console.debug('[ws] ←', msg.type, msg)
      const typeHandlers = handlers.get(msg.type)
      if (typeHandlers) typeHandlers.forEach(h => h(msg))

      const wildcardHandlers = handlers.get('*')
      if (wildcardHandlers) wildcardHandlers.forEach(h => h(msg))

      handlePeerLifecycle(msg)
    })

    socket.addEventListener('close', (event) => {
      console.warn('[ws] disconnected. code:', event.code, 'reason:', event.reason)
      setConnectionState(CONNECTION_STATE.DISCONNECTED)
      setWsConnection(null)
      socket = null
      if (event.code !== 1000 && reconnectAttempts < MAX_RECONNECT_ATTEMPTS) {
        const delay = RECONNECT_BASE_DELAY_MS * Math.pow(2, reconnectAttempts)
        reconnectAttempts++
        console.log(`[ws] reconnecting in ${delay}ms (attempt ${reconnectAttempts})`)
        reconnectTimer = setTimeout(() => ws.connect(), delay)
      }
    })

    socket.addEventListener('error', (err) => {
      console.error('[ws] error:', err)
      setConnectionState(CONNECTION_STATE.ERROR)
    })
  },
  send(message) {
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      console.warn('[ws] send() called but socket is not open. Message dropped:', message)
      return false
    }
    const data = JSON.stringify(message)
    console.debug('[ws] →', message.type, message)
    socket.send(data)
    return true
  },
  on(type, handler) {
    if (!handlers.has(type)) handlers.set(type, new Set())
    handlers.get(type).add(handler)

    return () => {
      handlers.get(type)?.delete(handler)
    }
  },

  off(type, handler) {
    handlers.get(type)?.delete(handler)
  },

  disconnect() {
    clearTimeout(reconnectTimer)
    reconnectAttempts = MAX_RECONNECT_ATTEMPTS 
    socket?.close(1000, 'user disconnect')
    socket = null
  },
  get readyState() {
    return socket?.readyState ?? WebSocket.CLOSED
  },
}
function handlePeerLifecycle(msg) {
  const { addPeer, removePeer, updatePeer } = useNodeStore.getState()

  switch (msg.type) {
    case 'peer_join':
      addPeer({
        id: msg.peerId,
        displayName: msg.displayName || msg.peerId,
        services: msg.services || [],
        lastSeen: new Date(),
        online: true,
      })
      break

    case 'peer_left':
      updatePeer(msg.peerId, { online: false })
      break

    case 'peer_list':
      // Server sends initial list of connected peers on join
      ;(msg.peers || []).forEach(peer => addPeer({ ...peer, online: true, lastSeen: new Date() }))
      break
  }
}

