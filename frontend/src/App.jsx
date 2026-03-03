import { useState, useEffect, useRef } from 'react'
import Shell from './components/Shell'
import Landing from './pages/Landing'
import ChatApp from './pages/ChatApp'
import VideoCall from './pages/VideoCall'
import PageTransition from './components/PageTransition'
import { useCursor, useMagnetic } from './hooks/useEffects'
import { saveAuth, saveScreen, loadAuth, loadScreen, isTokenExpired } from './lib/session'
import { Signaling } from './lib/signaling'
import wsClient from './lib/ws'
import './index.css'

function getUsername() {
  const k = 'ln_user'
  if (localStorage.getItem(k)) return localStorage.getItem(k)
  const adj  = ['silent','swift','neon','ghost','iron','void','echo','frost','cyber','dark']
  const noun = ['node','fox','hawk','wolf','lynx','pike','crow','sage','byte','flux']
  const name = adj[Math.random()*adj.length|0]+'_'+noun[Math.random()*noun.length|0]+'_'+(Math.random()*9000+1000|0)
  localStorage.setItem(k, name)
  return name
}

export default function App() {
  const savedAuth   = loadAuth()
  const savedScreen = loadScreen()
  const validAuth   = savedAuth && !isTokenExpired(savedAuth.token) ? savedAuth : null
  const initScreen  = validAuth ? savedScreen : 'home'

  const [screen,        setScreen]        = useState(initScreen)
  const [nextScreen,    setNextScreen]    = useState(null)
  const screenRef = useRef(initScreen)
  const [auth,          setAuth]          = useState(validAuth)
  const [authErr,       setAuthErr]       = useState('')
  const [nodeStatus,    setNode]          = useState(null)
  const [transitioning, setTransitioning] = useState(false)

  // Peers from ws singleton Map — zero duplicates possible
  const [peers, setPeers] = useState([])

  // Global incoming call state — shown over ANY screen
  const [incomingCall, setIncomingCall] = useState(null) // { from, fromName, sdp, audioOnly }
  const sigRef = useRef(null)

  const username = getUsername()
  useCursor()
  useMagnetic()

  // (reload transition removed — caused blank screen on refresh)

  // Health poll
  useEffect(() => {
    const poll = () => fetch('http://localhost:8080/health').then(r=>r.json()).then(setNode).catch(()=>setNode(null))
    poll(); const id = setInterval(poll, 5000); return () => clearInterval(id)
  }, [])

  // Subscribe to deduplicated peer list from ws singleton
  useEffect(() => wsClient.onPeers(setPeers), [])

  // Global incoming call listener — active regardless of current screen
  useEffect(() => {
    if (!auth?.node_id) return
    const sig = new Signaling(wsClient, auth.node_id)
    sigRef.current = sig

    sig.onOffer = ({ from, fromName, sdp, audioOnly }) => {
      // If VideoCall is already mounted (screen === 'video'),
      // it handles the offer directly via its own Signaling instance.
      // App only shows the overlay when user is on another screen.
      if (screenRef.current === 'video') return
      setIncomingCall({ from, fromName, sdp, audioOnly })
    }

    return () => { sig.destroy(); sigRef.current = null }
  }, [auth?.node_id])

  const acceptCall = () => {
    // Navigate to video screen — VideoCall will handle the answer
    navigate('video')
    // Pass the pending offer via a ref so VideoCall can pick it up
    pendingOfferRef.current = incomingCall
    setIncomingCall(null)
  }

  const rejectCall = () => {
    if (incomingCall?.from) sigRef.current?.sendReject(incomingCall.from)
    setIncomingCall(null)
  }

  const pendingOfferRef = useRef(null)

  const navigate = async (dest) => {
    if (dest === screen) return
    let newAuth = auth
    if (dest !== 'home' && (!auth || isTokenExpired(auth?.token))) {
      try {
        setAuthErr('')
        const res = await fetch('http://localhost:8080/api/auth', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ display_name: username, services: ['chat','video','drive'] }),
        })
        if (!res.ok) throw new Error(await res.text())
        newAuth = await res.json()
        setAuth(newAuth); saveAuth(newAuth)
      } catch (e) { setAuthErr(e.message); return }
    }
    setNextScreen(dest)
    setTransitioning(true)
  }

  const onTransitionDone = () => {
    if (nextScreen) {
      setScreen(nextScreen)
      saveScreen(nextScreen)
      screenRef.current = nextScreen
    }
    setTransitioning(false)
    setNextScreen(null)
  }

  const nodeID        = auth?.node_id ?? nodeStatus?.node
  const displayScreen = nextScreen || screen

  return (
    <>
      <div className="cursor-dot" />
      <div className="cursor-ring" />
      <div className="grain" aria-hidden />

      <Shell active={displayScreen === 'home' ? 'home' : displayScreen} onNav={navigate} nodeID={nodeID}>
        <PageTransition active={transitioning} onDone={onTransitionDone}>
          {screen === 'home' && (
            <Landing username={username} onSelect={navigate} error={authErr} nodeStatus={nodeStatus} />
          )}
          {screen === 'chat' && auth && (
            <ChatApp auth={auth} username={username} />
          )}
          {screen === 'video' && auth && (
            <VideoCall
              auth={auth} peers={peers} wsClient={wsClient}
              incomingCall={pendingOfferRef.current}
              onCallHandled={() => { pendingOfferRef.current = null }}
            />
          )}
          {(screen === 'files' || screen === 'drive') && (
            <ComingSoon feature={screen} onBack={() => navigate('home')} />
          )}
        </PageTransition>
      </Shell>

      {/* Global incoming call overlay — shows over ANY screen */}
      {incomingCall && (
        <div style={ov.overlay}>
          <div style={ov.panel}>
            <div style={ov.pulse}>
              <div style={ov.ring} />
              <div style={ov.core} />
            </div>
            <div style={ov.label}>INCOMING CALL</div>
            <div style={ov.name}>@{incomingCall.fromName || incomingCall.from}</div>
            <div style={ov.type}>{incomingCall.audioOnly ? 'AUDIO ONLY' : 'VIDEO + AUDIO'}</div>
            <div style={ov.btns}>
              <button style={ov.answerBtn} onClick={acceptCall}>ANSWER</button>
              <button style={ov.rejectBtn} onClick={rejectCall}>DECLINE</button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}

const ov = {
  overlay: { position:'fixed', inset:0, zIndex:9000, background:'rgba(7,7,7,0.92)', display:'flex', alignItems:'center', justifyContent:'center', fontFamily:'var(--mono)' },
  panel:   { display:'flex', flexDirection:'column', alignItems:'center', gap:'1rem', padding:'2.5rem 3rem', border:'1px solid rgba(184,255,53,0.2)', background:'#0a0a0a' },
  pulse:   { position:'relative', width:80, height:80, marginBottom:'0.5rem' },
  ring:    { position:'absolute', inset:0, borderRadius:'50%', border:'2px solid #b8ff35', animation:'pulseRing 1.4s ease-out infinite' },
  core:    { position:'absolute', inset:14, borderRadius:'50%', background:'#b8ff35', opacity:0.9 },
  label:   { fontFamily:'var(--display)', fontSize:'1.4rem', letterSpacing:'0.2em', color:'var(--text)' },
  name:    { fontFamily:'var(--display)', fontSize:'1.8rem', letterSpacing:'0.1em', color:'#b8ff35' },
  type:    { fontSize:'0.6rem', color:'var(--dim)', letterSpacing:'0.15em' },
  btns:    { display:'flex', gap:'1rem', marginTop:'0.5rem' },
  answerBtn: { fontFamily:'var(--mono)', fontSize:'0.7rem', letterSpacing:'0.12em', padding:'0.65rem 2rem', background:'#b8ff35', color:'#070707', border:'none', fontWeight:700 },
  rejectBtn: { fontFamily:'var(--mono)', fontSize:'0.7rem', letterSpacing:'0.12em', padding:'0.65rem 2rem', background:'rgba(255,47,47,0.08)', color:'#ff2f2f', border:'1px solid rgba(255,47,47,0.3)' },
}

function ComingSoon({ feature, onBack }) {
  return (
    <div style={{ display:'flex', flexDirection:'column', alignItems:'center', justifyContent:'center', height:'100%', gap:'1.5rem', fontFamily:'var(--mono)' }}>
      <span style={{ fontFamily:'var(--display)', fontSize:'4rem', color:'var(--faint)', letterSpacing:'0.1em' }}>{feature.toUpperCase()}</span>
      <span style={{ fontSize:'0.65rem', color:'var(--faint)', letterSpacing:'0.2em' }}>COMING SOON</span>
      <button data-mag="0.3" onClick={onBack} style={{ background:'none', border:'1px solid var(--border)', color:'var(--dim)', fontFamily:'var(--mono)', fontSize:'0.7rem', letterSpacing:'0.1em', padding:'0.5rem 1.5rem' }}>← BACK</button>
    </div>
  )
}