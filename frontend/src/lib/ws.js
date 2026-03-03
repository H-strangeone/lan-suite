

const WS_URL = 'ws://localhost:8080/ws'

export const STATE = Object.freeze({
  DISCONNECTED: 'disconnected',
  CONNECTING:   'connecting',
  CONNECTED:    'connected',
  RECONNECTING: 'reconnecting',
})

class WSClient {
  constructor() {
    this.ws          = null
    this.state       = STATE.DISCONNECTED
    this.token       = null
    this.nodeID      = null
    this.displayName = null
    this.services    = []
    this.currentRoom = null
    this._connID     = 0
    this._peers    = new Map()
    this._peerSubs = new Set()

    this._handlers          = new Map()
    this._reconnectAttempts = 0
    this._maxReconnects     = 10
    this._reconnectTimer    = null
    this._reconnectDebounce = null
  }

  connect(token, nodeID, displayName, services = ['chat']) {

    if (this.nodeID === nodeID && this.ws) {
      const s = this.ws.readyState
      if (s === WebSocket.OPEN || s === WebSocket.CONNECTING) return
    }

    this.token       = token
    this.nodeID      = nodeID
    this.displayName = displayName
    this.services    = services


    this._connID++
    const myID = this._connID

    if (this.ws) {
      const old = this.ws; this.ws = null
      try { old.close(1000, 'reconnecting') } catch {}
    }
    clearTimeout(this._reconnectTimer)
    clearTimeout(this._reconnectDebounce)
    this._reconnectAttempts = 0

    this._setState(STATE.CONNECTING)
    this._openSocket(myID)
  }

  disconnect() {
    this._connID++
    clearTimeout(this._reconnectTimer)
    clearTimeout(this._reconnectDebounce)
    this._reconnectAttempts = this._maxReconnects
    if (this.ws) {
      const ws = this.ws; this.ws = null
      try { ws.close(1000, 'user-disconnect') } catch {}
    }
    this._peers.clear()
    this._emitPeers()
    this._setState(STATE.DISCONNECTED)
  }

  joinRoomByID(roomID, password = '') {
    this.currentRoom = roomID
    this._send({ type: 'join_by_id', payload: { room_id: roomID, password: password || '' } })
  }

  leaveRoom() {
    this._send({ type: 'leave' })
    this.currentRoom = null
    this._peers.clear()
    this._emitPeers()
  }

  sendChat(text) {
    if (!this.currentRoom) return
    this._send({ type: 'chat_msg', room: this.currentRoom, payload: { text } })
  }

  requestRoomList()        { this._send({ type: 'room_list_req' }) }
  createRoom(n, pw = '')   { this._send({ type: 'create_room', payload: { name: n, password: pw } }) }
  kickMember(nodeID)       { this._send({ type: 'kick_member', payload: { node_id: nodeID } }) }
  disbandRoom()            { this._send({ type: 'disband_room', payload: {} }) }
  sendSignal(type, to, pl) { this._send({ type, to, payload: pl || {} }) }


  onPeers(fn) {
    this._peerSubs.add(fn)
    fn(this._getPeers())
    return () => this._peerSubs.delete(fn)
  }

  clearPeers() {
    this._peers.clear()
    this._emitPeers()
  }

  on(type, handler) {
    if (!this._handlers.has(type)) this._handlers.set(type, new Set())
    this._handlers.get(type).add(handler)
    return () => this._handlers.get(type)?.delete(handler)
  }

  _openSocket(myID) {
    const ws = new WebSocket(`${WS_URL}?token=${encodeURIComponent(this.token)}`)
    this.ws = ws

    ws.addEventListener('open', () => {
      if (myID !== this._connID) { try { ws.close(1000, 'stale') } catch {}; return }
      if (ws.readyState !== WebSocket.OPEN) return
      this._reconnectAttempts = 0
      clearTimeout(this._reconnectDebounce)
   
      ws.send(JSON.stringify({
        type: 'hello',
        payload: { display_name: this.displayName, node_id: this.nodeID, services: this.services },
      }))
     
      if (this.currentRoom && this._reconnectAttempts > 0) {
        ws.send(JSON.stringify({
          type: 'join_by_id',
          payload: { room_id: this.currentRoom, password: '' },
        }))
      }
      
      this._setState(STATE.CONNECTED)
    })

    ws.addEventListener('message', (event) => {
      if (myID !== this._connID) return
      let msg
      try { msg = JSON.parse(event.data) } catch { return }
      if (msg.payload && typeof msg.payload === 'string') {
        try { msg.payload = JSON.parse(msg.payload) } catch {}
      }
      if (!msg.from && msg.payload?.from_node_id) msg.from = msg.payload.from_node_id
      const isLan = msg.room === '__lan__'

      switch (msg.type) {
        case 'peer_list':
          if (!isLan) {
            this._peers.clear()
            for (const p of (msg.payload?.peers ?? []))
              if (p.node_id !== this.nodeID) this._peers.set(p.node_id, p)
            this._emitPeers()
          }
          break
        case 'peer_join':
          if (!isLan) {
            const p = msg.payload?.peer
            if (p && p.node_id !== this.nodeID) {
              this._peers.set(p.node_id, p) 
              this._emitPeers()
            }
          }
          break
        case 'peer_left':
          if (!isLan) {
            const id = msg.payload?.node_id
            if (id && this._peers.delete(id)) this._emitPeers()
          }
          break
      }

      if (!isLan) this._emit(msg.type, msg)
    })

    ws.addEventListener('close', (e) => {
      if (myID !== this._connID) return
      this.ws = null
      
      if (e.code === 1000 || e.code === 1001 || e.code === 1005) {
        this._setState(STATE.DISCONNECTED)
        return
      }
      this._scheduleReconnect(myID)
    })

    ws.addEventListener('error', (e) => {
      if (myID !== this._connID) return
      console.error('[ws] socket error', e)
    })
  }

  _scheduleReconnect(myID) {
    if (this._reconnectAttempts >= this._maxReconnects) {
      this._setState(STATE.DISCONNECTED); return
    }
    this._reconnectAttempts++
    const delay = Math.min(2 ** this._reconnectAttempts * 1000, 30_000) + Math.random() * 500
 
    this._reconnectDebounce = setTimeout(() => this._setState(STATE.RECONNECTING), 2000)
    this._reconnectTimer = setTimeout(() => {
      clearTimeout(this._reconnectDebounce)
      if (myID === this._connID) this._openSocket(myID)
    }, delay)
  }

  _send(msg) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(msg))
    else console.warn('[ws] not open, dropping:', msg.type)
  }
  _emit(type, data) {
    this._handlers.get(type)?.forEach(h => { try { h(data) } catch(e) { console.error(`[ws] ${type}:`, e) } })
  }
  _setState(s) {
    if (this.state === s) return
    this.state = s
    this._emit('state', { state: s })
  }
  _getPeers()  { return Array.from(this._peers.values()) }
  _emitPeers() { const a = this._getPeers(); this._peerSubs.forEach(fn => { try { fn(a) } catch {} }) }
}

export const wsClient = new WSClient()
export default wsClient