

const CCN_BASE = '/video/calls'

export class Signaling {
  /**
   * @param {import('./ws').WSClient} ws  - the singleton wsClient
   * @param {string} nodeID               - our node ID (for CCN naming)
   * @param {object} [opts]
   * @param {boolean} [opts.ccn]          - enable CCN path (Phase 2, default false)
   */
  constructor(ws, nodeID, opts = {}) {
    this._ws     = ws
    this._nodeID = nodeID
    this._ccn    = opts.ccn ?? false   // flip to true when backend is ready
    this._unsub  = []

    // ── Public callbacks ──────────────────────────────────────────────────────
    /** @type {(data:{from:string, fromName:string, sdp:RTCSessionDescriptionInit, audioOnly:boolean})=>void} */
    this.onOffer  = null
    /** @type {(data:{from:string, sdp:RTCSessionDescriptionInit})=>void} */
    this.onAnswer = null
    /** @type {(data:{from:string, candidate:RTCIceCandidateInit})=>void} */
    this.onICE    = null
    /** @type {(data:{from:string})=>void} */
    this.onHangup = null
    /** @type {(data:{from:string})=>void} */
    this.onReject = null

    this._attachWS()
  }


  sendOffer(targetID, sdp, audioOnly = false, fromName = '') {
    const payload = { sdp, audio_only: audioOnly, from_name: fromName, from_node_id: this._nodeID }
    if (this._ccn) {
     
      console.warn('[sig] CCN not yet active, falling back to WS')
    }
    this._ws.sendSignal('offer', targetID, payload)
  }

  sendAnswer(targetID, sdp) {
    const payload = { sdp, from_node_id: this._nodeID }
    this._ws.sendSignal('answer', targetID, payload)
  }

  sendICE(targetID, candidate) {
    const payload = { candidate, from_node_id: this._nodeID }
    this._ws.sendSignal('ice', targetID, payload)
  }

  sendHangup(targetID) {
    this._ws.sendSignal('call_hangup', targetID, { from_node_id: this._nodeID })
  }

  sendReject(targetID) {
    this._ws.sendSignal('call_reject', targetID, { from_node_id: this._nodeID })
  }



  _attachWS() {
    const _on = (type, fn) => {
      const unsub = this._ws.on(type, fn)
      this._unsub.push(unsub)
    }

    _on('offer', msg => {
      const p = this._parsePayload(msg)
      if (!p?.sdp) return
      const from = msg.from || p.from_node_id || ''
      this.onOffer?.({ from, fromName: p.from_name || from, sdp: p.sdp, audioOnly: !!p.audio_only })
    })

    _on('answer', msg => {
      const p = this._parsePayload(msg)
      if (!p?.sdp) return
      const from = msg.from || p.from_node_id || ''
      this.onAnswer?.({ from, sdp: p.sdp })
    })

    _on('ice', msg => {
      const p = this._parsePayload(msg)
      if (!p?.candidate) return
      const from = msg.from || p.from_node_id || ''
      this.onICE?.({ from, candidate: p.candidate })
    })

    _on('call_hangup', msg => {
      const from = msg.from || this._parsePayload(msg)?.from_node_id || ''
      this.onHangup?.({ from })
    })

    _on('call_reject', msg => {
      const from = msg.from || this._parsePayload(msg)?.from_node_id || ''
      this.onReject?.({ from })
    })
  }

  _parsePayload(msg) {
    const p = msg.payload
    if (!p) return null
    if (typeof p === 'object') return p
    try { return JSON.parse(p) } catch { return null }
  }

  destroy() {
    this._unsub.forEach(f => f())
    this._unsub = []
  }
}


export function preferOpus(sdp) {
  const lines = sdp.split('\r\n')
  let opusPT  = null

  // Find Opus payload type
  for (const line of lines) {
    const m = line.match(/^a=rtpmap:(\d+) opus\/48000/i)
    if (m) { opusPT = m[1]; break }
  }
  if (!opusPT) return sdp  // no Opus → return unchanged

  const result = []
  let inAudio  = false

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]

    if (line.startsWith('m=audio')) {
      inAudio = true
      // Reorder: put Opus PT first in the payload list
      result.push(reorderPayloads(line, opusPT))
      continue
    }

    if (line.startsWith('m=') && !line.startsWith('m=audio')) {
      inAudio = false
    }

    if (inAudio && line.startsWith(`a=fmtp:${opusPT}`)) {
      // Inject/merge our Opus parameters
      const existing = line.includes('=') ? line.split(' ').slice(1).join(' ') : ''
      const params   = mergeOpusParams(existing)
      result.push(`a=fmtp:${opusPT} ${params}`)
      continue
    }

    result.push(line)
  }

  // If there was no fmtp line for Opus yet, insert one after the rtpmap
  if (!sdp.includes(`a=fmtp:${opusPT}`)) {
    const rtpIdx = result.findIndex(l => l.startsWith(`a=rtpmap:${opusPT}`))
    if (rtpIdx !== -1) {
      const params = mergeOpusParams('')
      result.splice(rtpIdx + 1, 0, `a=fmtp:${opusPT} ${params}`)
    }
  }

  return result.join('\r\n')
}

function mergeOpusParams(existing) {
  const map = {}
  if (existing) {
    existing.split(';').forEach(kv => {
      const [k, v] = kv.split('=').map(s => s.trim())
      if (k) map[k] = v ?? '1'
    })
  }
  // Apply our overrides
  map['useinbandfec']      = '1'
  map['maxaveragebitrate'] = '128000'
  map['stereo']            = '1'
  map['sprop-stereo']      = '1'
  map['minptime']          = '10'
  return Object.entries(map).map(([k, v]) => `${k}=${v}`).join(';')
}

function reorderPayloads(mLine, preferredPT) {
  // m=audio 9 UDP/TLS/RTP/SAVPF 111 103 104 ...
  const parts = mLine.split(' ')
  const head  = parts.slice(0, 3)         // m=audio 9 UDP/...
  const pts   = parts.slice(3)            // payload types
  const idx   = pts.indexOf(preferredPT)
  if (idx > 0) {
    pts.splice(idx, 1)
    pts.unshift(preferredPT)
  }
  return [...head, ...pts].join(' ')
}