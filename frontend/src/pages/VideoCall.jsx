/**
 * VideoCall — pages/VideoCall.jsx
 * Full-page WebRTC video/audio calling with LN aesthetic.
 * Uses CSS module (VideoCall.module.css) — no inline styles.
 */

import { useState, useEffect, useRef, useCallback } from 'react'
import { Signaling, preferOpus } from '../lib/signaling'
import _wsClient from '../lib/ws'
import s from './VideoCall.module.css'

const ICE_SERVERS = [
  { urls: 'stun:stun.l.google.com:19302' },
  { urls: 'stun:stun1.l.google.com:19302' },
]

const CS = Object.freeze({
  IDLE:       'idle',
  CALLING:    'calling',
  INCOMING:   'incoming',
  CONNECTING: 'connecting',
  CONNECTED:  'connected',
  ENDED:      'ended',
})

const SPEED_LINES = Array.from({ length: 16 }, (_, i) => ({
  top:   `${(i / 16) * 100}%`,
  delay: `${((i * 0.13) % 2).toFixed(2)}s`,
  width: `${38 + (i % 6) * 10}%`,
  opacity: 0.012 + (i % 4) * 0.007,
}))

export default function VideoCall({ auth, peers: peersProp = [], wsClient: wsClientProp, incomingCall, onCallHandled }) {
  const wsClient = wsClientProp || _wsClient
  const [callState,  setCallState]  = useState(CS.IDLE)
  const [remotePeer, setRemotePeer] = useState(null)
  const [audioOnly,  setAudioOnly]  = useState(false)
  const [muted,      setMuted]      = useState(false)
  const [camOff,     setCamOff]     = useState(false)
  const [error,      setError]      = useState(null)
  const [iceState,   setIceState]   = useState('')
  const [elapsed,    setElapsed]    = useState(0)
  // Subscribe to peers from singleton — zero duplicates, __lan__ filtered
  const [peers, setPeers] = useState(() => _wsClient._getPeers?.() ?? peersProp)
  useEffect(() => {
    return _wsClient.onPeers(setPeers)
  }, [])

  // Join the saved room when VideoCall mounts so we get a peer_list.
  // Without this, going directly to /video (or refreshing) shows no peers
  // because peer_list only arrives after a room join.
  // We use the same room ChatApp uses (saved in sessionStorage).
  useEffect(() => {
    if (!auth?.token || !auth?.node_id) return
    // Connect if not already (e.g. direct page load to video tab)
    _wsClient.connect(auth.token, auth.node_id,
      auth.display_name || auth.node_id, ['chat', 'video'])
    // Join room on connect (or immediately if already connected)
    const joinSavedRoom = () => {
      const savedRoom = sessionStorage.getItem('ls_room') || 'general'
      if (_wsClient.currentRoom !== savedRoom) {
        _wsClient.joinRoomByID(savedRoom, '')
      }
    }
    if (_wsClient.state === 'connected') {
      joinSavedRoom()
    } else {
      const unsub = _wsClient.on('state', ({ state }) => {
        if (state === 'connected') { joinSavedRoom(); unsub() }
      })
      return unsub
    }
  }, [auth?.node_id])

  const pcRef         = useRef(null)
  const sigRef        = useRef(null)
  const callStateRef  = useRef(CS.IDLE)
  const peersRef      = useRef([])
  const offerHandledRef = useRef(false) // prevents double-processing via both prop and sig.onOffer
  const localStream   = useRef(null)
  const remoteStream  = useRef(null)
  const localVideoEl  = useRef(null)
  const remoteVideoEl = useRef(null)
  const pendingOffer  = useRef(null)
  const timerRef      = useRef(null)

  // Keep refs in sync for use inside stable callbacks
  useEffect(() => { callStateRef.current = callState }, [callState])
  useEffect(() => { peersRef.current = peers }, [peers])

  // ── Call timer ─────────────────────────────────────────────────────────────
  useEffect(() => {
    if (callState === CS.CONNECTED) {
      setElapsed(0)
      timerRef.current = setInterval(() => setElapsed(e => e + 1), 1000)
    } else {
      clearInterval(timerRef.current)
    }
    return () => clearInterval(timerRef.current)
  }, [callState])

  const fmt = (sec) => {
    const m  = String(Math.floor(sec / 60)).padStart(2, '0')
    const ss = String(sec % 60).padStart(2, '0')
    return `${m}:${ss}`
  }

  // ── Cleanup ────────────────────────────────────────────────────────────────
  const cleanup = useCallback(() => {
    // Stop all tracks immediately — camera/mic light turns off right away
    localStream.current?.getTracks().forEach(t => { t.stop(); t.enabled = false })
    localStream.current = null
    pcRef.current?.close()
    pcRef.current = null
    if (localVideoEl.current)  { localVideoEl.current.srcObject  = null }
    if (remoteVideoEl.current) { remoteVideoEl.current.srcObject = null }
    remoteStream.current?.getTracks().forEach(t => t.stop())
    remoteStream.current = null
    pendingOffer.current = null
    setCallState(CS.IDLE)
    setRemotePeer(null)
    setMuted(false)
    setCamOff(false)
    setIceState('')
    setError(null)
  }, [])

  // ── Consume incoming call forwarded from App level ─────────────────────────
  // App.jsx listens globally for 'offer' and navigates here, passing the call.
  // We pick it up here and trigger the INCOMING state — no double-listen needed
  // because App's listener fires before VideoCall mounts on nav to video screen.
  useEffect(() => {
    if (!incomingCall || callState !== CS.IDLE) return
    if (offerHandledRef.current) {
      // sig.onOffer already handled this offer directly (user was on video screen)
      offerHandledRef.current = false
      onCallHandled?.()
      return
    }
    const { from, fromName, sdp, audioOnly: ao } = incomingCall
    const fromPeer = peers.find(p => p.node_id === from) ||
                     { node_id: from, display_name: fromName || from }
    pendingOffer.current = { sdp, audioOnly: ao, from: fromPeer }
    setRemotePeer(fromPeer)
    setCallState(CS.INCOMING)
    onCallHandled?.()  // clear from App so it doesn't re-trigger on re-render
  }, [incomingCall])  // eslint-disable-line

  // ── Signaling ──────────────────────────────────────────────────────────────
  useEffect(() => {
    if (!wsClient || !auth?.node_id) return
    const sig = new Signaling(wsClient, auth.node_id)
    sigRef.current = sig

    // Handle incoming offers directly on video page
    // (when user is already on /video, App overlay fires AND this fires —
    //  the incomingCall useEffect below deduplicates via callState guard)
    sig.onOffer = ({ from, fromName, sdp, audioOnly: ao }) => {
      if (callStateRef.current !== CS.IDLE) return // already in a call
      const fromPeer = peersRef.current.find(p => p.node_id === from) ||
                       { node_id: from, display_name: fromName || from }
      pendingOffer.current = { sdp, audioOnly: ao, from: fromPeer }
      offerHandledRef.current = true // tell incomingCall prop effect to skip
      setRemotePeer(fromPeer)
      setCallState(CS.INCOMING)
    }

    sig.onAnswer = async ({ sdp }) => {
      if (!pcRef.current) return
      setCallState(CS.CONNECTING)
      try { await pcRef.current.setRemoteDescription(new RTCSessionDescription(sdp)) }
      catch (e) { setError('Remote description failed: ' + e.message) }
    }

    sig.onICE = async ({ candidate }) => {
      if (!pcRef.current) return
      try { await pcRef.current.addIceCandidate(new RTCIceCandidate(candidate)) } catch {}
    }

    sig.onHangup = () => {
      cleanup() // stops camera/mic immediately
      setCallState(CS.ENDED)
      setTimeout(() => setCallState(CS.IDLE), 2500)
    }

    sig.onReject = () => {
      cleanup() // stops camera/mic immediately
      setCallState(CS.ENDED)
      setError('Call declined by remote peer.')
      setTimeout(() => { setCallState(CS.IDLE); setError(null) }, 3000)
    }

    return () => { sig.destroy(); sigRef.current = null }
  }, [wsClient, auth?.node_id, cleanup])

  // ── RTCPeerConnection ──────────────────────────────────────────────────────
  const createPC = useCallback((targetNodeId) => {
    pcRef.current?.close()
    const pc = new RTCPeerConnection({ iceServers: ICE_SERVERS })
    pcRef.current = pc

    localStream.current?.getTracks().forEach(t => pc.addTrack(t, localStream.current))

    pc.onicecandidate = ({ candidate }) => {
      if (candidate) sigRef.current?.sendICE(targetNodeId, candidate)
    }
    pc.oniceconnectionstatechange = () => {
      const st = pc.iceConnectionState
      setIceState(st)
      if (st === 'connected' || st === 'completed') setCallState(CS.CONNECTED)
      if (st === 'failed'    || st === 'disconnected') {
        setError('Connection lost.')
        setCallState(CS.ENDED)
        setTimeout(() => cleanup(), 2000)
      }
    }
    const rs = new MediaStream()
    remoteStream.current = rs
    pc.ontrack = ({ track }) => {
      rs.addTrack(track)
      if (remoteVideoEl.current) remoteVideoEl.current.srcObject = rs
    }
    return pc
  }, [cleanup])

  const getMedia = useCallback(async (withVideo) => {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true, sampleRate: 48000, channelCount: 2 },
        video: withVideo ? { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 30 } } : false,
      })
      localStream.current = stream
      if (localVideoEl.current) localVideoEl.current.srcObject = stream
      return stream
    } catch (e) {
      setError(`Media error: ${e.message}`)
      return null
    }
  }, [])

  // ── Outgoing ───────────────────────────────────────────────────────────────
  const startCall = useCallback(async (peer, withVideo) => {
    if (callState !== CS.IDLE) return
    setRemotePeer(peer)
    setAudioOnly(!withVideo)
    setCallState(CS.CALLING)
    const stream = await getMedia(withVideo)
    if (!stream) { setCallState(CS.IDLE); return }
    const pc = createPC(peer.node_id)
    try {
      const offer  = await pc.createOffer()
      const munged = { type: offer.type, sdp: preferOpus(offer.sdp) }
      await pc.setLocalDescription(munged)
      sigRef.current?.sendOffer(peer.node_id, munged, !withVideo, auth?.node_id)
    } catch (e) { setError(`Offer failed: ${e.message}`); cleanup() }
  }, [callState, getMedia, createPC, auth, cleanup])

  // ── Incoming ───────────────────────────────────────────────────────────────
  const answerCall = useCallback(async () => {
    const { sdp, audioOnly: ao, from } = pendingOffer.current || {}
    if (!sdp || !from) return
    setCallState(CS.CONNECTING)
    setAudioOnly(ao)
    const stream = await getMedia(!ao)
    if (!stream) { cleanup(); return }
    const pc = createPC(from.node_id)
    try {
      await pc.setRemoteDescription(new RTCSessionDescription(sdp))
      const answer = await pc.createAnswer()
      const munged = { type: answer.type, sdp: preferOpus(answer.sdp) }
      await pc.setLocalDescription(munged)
      sigRef.current?.sendAnswer(from.node_id, munged)
    } catch (e) { setError(`Answer failed: ${e.message}`); cleanup() }
  }, [getMedia, createPC, cleanup])

  const rejectCall = useCallback(() => {
    const { from } = pendingOffer.current || {}
    if (from) sigRef.current?.sendReject(from.node_id)
    cleanup() // stops camera/mic immediately
    setCallState(CS.ENDED)
    setTimeout(() => setCallState(CS.IDLE), 1500)
  }, [cleanup])

  const hangUp = useCallback(() => {
    if (remotePeer) sigRef.current?.sendHangup(remotePeer.node_id)
    cleanup()
    setCallState(CS.ENDED)
    setTimeout(() => setCallState(CS.IDLE), 2000)
  }, [remotePeer, cleanup])

  const toggleMute = () => {
    localStream.current?.getAudioTracks().forEach(t => { t.enabled = !t.enabled })
    setMuted(m => !m)
  }
  const toggleCam = () => {
    localStream.current?.getVideoTracks().forEach(t => { t.enabled = !t.enabled })
    setCamOff(c => !c)
  }

  // peers from wsClient._peers already excludes self (filtered in ws.js),
  // but double-guard here for safety. Also filter out is_self marker if present.
  const callablePeers = peers.filter(p => p.node_id !== auth?.node_id && !p.is_self)
  const isActive      = callState === CS.CONNECTING || callState === CS.CONNECTED

  // ── Render ─────────────────────────────────────────────────────────────────
  return (
    <div className={s.page}>

      {/* Speed lines */}
      <div className={s.speedLines} aria-hidden>
        {SPEED_LINES.map((l, i) => (
          <div key={i} className={s.line} style={{
            top: l.top, animationDelay: l.delay, width: l.width, opacity: l.opacity,
          }} />
        ))}
      </div>

      {/* Header */}
      <header className={s.header}>
        <div className={s.headerLeft}>
          <span className={s.headerIcon}>◉</span>
          <span className={s.headerTitle}>VIDEO CALL</span>
          <span className={s.headerBadge}>P2P · WEBRTC</span>
        </div>
        <div className={s.headerRight}>
          <span className={s.codecBadge}>OPUS 128K · FEC · STEREO</span>
          <span className={s.codecBadge}>H.264 · 720P · 30FPS</span>
        </div>
      </header>

      {/* Body */}
      <div className={s.body}>

        {/* IDLE */}
        {callState === CS.IDLE && (
          <div className={s.idlePane}>
            <div className={s.idleHero}>
              <div className={s.idleLabel}>READY TO CONNECT</div>
              <h1 className={s.idleHeading}>
                <span className={s.idleHeadingAccent}>CALL </span>A PEER
              </h1>
              <p className={s.idleSub}>
                Direct peer-to-peer connection · No relay · No cloud
              </p>
            </div>

            <div className={s.peerList}>
              <div className={s.peerListHead}>PEERS ONLINE · {callablePeers.length}</div>

              {callablePeers.length === 0 ? (
                <div className={s.emptyPeers}>
                  <div className={s.emptyLabel}>NO PEERS ONLINE</div>
                  <p className={s.emptyHint}>
                    Waiting for someone to join the room…<br />
                    <span>Join the same chat room first</span>, then come here to call.
                  </p>
                </div>
              ) : callablePeers.map((p, i) => (
                <div key={p.node_id} className={s.peerRow} style={{ animationDelay: `${i * 0.05}s` }}>
                  <span className={s.peerDot} />
                  <div className={s.peerInfo}>
                    <div className={s.peerName}>@{p.display_name}</div>
                    <div className={s.peerSvc}>{p.services?.join(' · ').toUpperCase() || 'ONLINE'}</div>
                  </div>
                  <div className={s.callBtns}>
                    <button className={s.callBtn} onClick={() => startCall(p, true)}>
                      <span className={s.callBtnIcon}>📹</span>VIDEO
                    </button>
                    <button className={s.callBtn} onClick={() => startCall(p, false)}>
                      <span className={s.callBtnIcon}>📞</span>AUDIO
                    </button>
                  </div>
                </div>
              ))}
            </div>

            <div className={s.infoStrip}>
              <div className={s.infoItem}>
                <span className={s.infoLabel}>AUDIO CODEC</span>
                <span className={`${s.infoValue} ${s.green}`}>OPUS 128K</span>
              </div>
              <div className={s.infoDivider} />
              <div className={s.infoItem}>
                <span className={s.infoLabel}>ERROR CORRECTION</span>
                <span className={`${s.infoValue} ${s.green}`}>FEC ON</span>
              </div>
              <div className={s.infoDivider} />
              <div className={s.infoItem}>
                <span className={s.infoLabel}>PTIME</span>
                <span className={s.infoValue}>10MS</span>
              </div>
              <div className={s.infoDivider} />
              <div className={s.infoItem}>
                <span className={s.infoLabel}>ROUTING</span>
                <span className={s.infoValue}>DIRECT P2P</span>
              </div>
              <div className={s.infoDivider} />
              <div className={s.infoItem}>
                <span className={s.infoLabel}>SIGNALING</span>
                <span className={s.infoValue}>WS → CCN</span>
              </div>
            </div>
          </div>
        )}

        {/* ACTIVE CALL */}
        {isActive && (
          <div className={s.callLayout}>
            <div className={s.remoteArea}>
              <video ref={remoteVideoEl} autoPlay playsInline className={s.remoteVideo} />

              {audioOnly && (
                <div className={s.audioOnlyOverlay}>
                  <div className={s.avatar} style={{ width: 100, height: 100, fontSize: 42 }}>
                    {remotePeer?.display_name?.[0]?.toUpperCase() || '?'}
                  </div>
                  <div className={s.audioOnlyName}>@{remotePeer?.display_name}</div>
                  <div className={s.audioOnlyLabel}>AUDIO CALL · OPUS 128K</div>
                </div>
              )}

              <div className={s.remoteBadge}>
                <span className={s.remoteBadgeDot} />
                @{remotePeer?.display_name}
              </div>

              {callState === CS.CONNECTING && (
                <div className={s.connectingOverlay}>
                  <span className={s.connectingLabel}>
                    {iceState === 'checking' ? 'CHECKING ICE…' : 'CONNECTING…'}
                  </span>
                </div>
              )}

              {!audioOnly && (
                <video ref={localVideoEl} autoPlay playsInline muted className={s.localVideo} />
              )}
            </div>

            <div className={s.controls}>
              <CtrlBtn icon={muted  ? '🔇' : '🎙️'} label={muted  ? 'Unmute' : 'Mute'}     onClick={toggleMute} active={muted} />
              {!audioOnly && (
                <CtrlBtn icon={camOff ? '📵' : '📷'} label={camOff ? 'Cam on' : 'Cam off'} onClick={toggleCam}  active={camOff} />
              )}
              {callState === CS.CONNECTED && (
                <span className={s.callTimer}>{fmt(elapsed)}</span>
              )}
              <CtrlBtn icon="📞" label="Hang up" onClick={hangUp} danger />
            </div>
          </div>
        )}

        {/* OVERLAYS */}

        {callState === CS.CALLING && (
          <div className={s.overlay}>
            <Pulse color="var(--green)" />
            <div className={s.overlayLabel}>
              CALLING<br />
              <span style={{ color: 'var(--green)' }}>@{remotePeer?.display_name}</span>
            </div>
            <div className={s.overlaySub}>
              {audioOnly ? 'AUDIO ONLY · OPUS 128K' : 'VIDEO · H.264 720P · OPUS 128K'}
            </div>
            <div className={s.overlayBtns}>
              <button className={`${s.btn} ${s.btnRed}`} onClick={hangUp}>CANCEL</button>
            </div>
          </div>
        )}

        {callState === CS.INCOMING && (
          <div className={s.overlay}>
            <Pulse color="#4ade80" />
            <div className={s.overlayLabel}>
              INCOMING CALL<br />
              <span style={{ color: 'var(--green)' }}>@{remotePeer?.display_name}</span>
            </div>
            <div className={s.overlaySub}>
              {pendingOffer.current?.audioOnly ? 'AUDIO ONLY' : 'VIDEO + AUDIO'}
            </div>
            <div className={s.overlayBtns}>
              <button className={`${s.btn} ${s.btnGreen}`} onClick={answerCall}>ANSWER</button>
              <button className={`${s.btn} ${s.btnRed}`}   onClick={rejectCall}>DECLINE</button>
            </div>
          </div>
        )}

        {callState === CS.ENDED && (
          <div className={s.overlay}>
            <div className={`${s.overlayLabel} ${s.overlayEnded}`}>CALL ENDED</div>
          </div>
        )}

      </div>

      {/* Error banner */}
      {error && callState === CS.IDLE && (
        <div className={s.errorBanner}>
          <span>⚠ {error}</span>
          <button className={s.errorClose} onClick={() => setError(null)}>✕</button>
        </div>
      )}

    </div>
  )
}

function CtrlBtn({ icon, label, onClick, active, danger }) {
  return (
    <button
      onClick={onClick}
      title={label}
      className={[s.ctrlBtn, active ? s.ctrlBtnActive : '', danger ? s.ctrlBtnDanger : ''].join(' ')}
    >
      {icon}
    </button>
  )
}

function Pulse({ color = 'var(--green)' }) {
  return (
    <div className={s.pulse}>
      <div className={s.pulseRing} style={{ borderColor: color }} />
      <div className={s.pulseCore} style={{ background: color }} />
    </div>
  )
}