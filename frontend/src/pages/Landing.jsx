import { useEffect, useState } from 'react'
import { useMagnetic, useReveal } from '../hooks/useEffects'
import Footer from '../components/Footer'
import s from './Landing.module.css'

const MARQUEE = [
  'LAN CHAT','VIDEO CALLS','FILE TRANSFER','SHARED DRIVE',
  'ED25519','CCN ROUTING','ZERO CLOUD','NO INTERNET REQUIRED',
  'LAN CHAT','VIDEO CALLS','FILE TRANSFER','SHARED DRIVE',
  'ED25519','CCN ROUTING','ZERO CLOUD','NO INTERNET REQUIRED',
]

const FEATURES = [
  { id:'chat',  icon:'◈', label:'LAN CHAT',     desc:'Encrypted rooms. Persistent history. Offline-first delivery.',  status:'LIVE' },
  { id:'video', icon:'◉', label:'VIDEO CALL',   desc:'Peer-to-peer WebRTC. Direct device connection. No relay.',       status:'LIVE' },
  { id:'files', icon:'◫', label:'FILE SHARE',   desc:'QUIC chunked transfer. Any file size. Zstd compression.',        status:'SOON' },
  { id:'drive', icon:'◧', label:'SHARED DRIVE', desc:'Distributed namespace. CCN-cached. Offline-first sync.',          status:'SOON' },
]

export default function Landing({ username, onSelect, error, nodeStatus }) {
  const [time, setTime] = useState(new Date().toTimeString().slice(0,8))
  useMagnetic()
  useReveal()

  useEffect(() => {
    const id = setInterval(() => setTime(new Date().toTimeString().slice(0,8)), 1000)
    return () => clearInterval(id)
  }, [])

  return (
    <div className={s.page}>

      {/* Hero */}
      <section className={s.hero}>
        <div className={s.speedLines} aria-hidden>
          {Array.from({length:20}).map((_,i) => (
            <div key={i} className={s.line} style={{
              top: `${(i/20)*100}%`,
              animationDelay: `${(i*0.11)%1.4}s`,
              width: `${40 + (i%5)*12}%`,
              opacity: 0.018 + (i%4)*0.009,
            }}/>
          ))}
        </div>

        <div className={s.heroInner}>
          <div className={s.badge}>
            <span className={s.badgeDot}/>
            NODE {nodeStatus?.node ?? '--------'} &nbsp;·&nbsp; {time}
          </div>

          <h1 className={s.title}>
            <span className={`glitch ${s.word1}`} data-text="LAN">LAN</span>
            <span className={s.word2}>SUITE</span>
          </h1>

          <p className={s.desc}>
            Decentralized communication for local networks.<br/>
            No internet. No cloud. No tracking. Just your LAN.
          </p>

          <div className={s.tags}>
            {['CCN ROUTING','ED25519 IDENTITY','QUIC TRANSFER','ZERO CLOUD'].map(t => (
              <span key={t} className={s.tag}>{t}</span>
            ))}
          </div>
        </div>

        {nodeStatus && (
          <div className={s.statsBar}>
            <Stat label="WS CLIENTS" val={nodeStatus.ws_clients}/>
            <Stat label="LAN PEERS"  val={nodeStatus.lan_peers}/>
            <Stat label="CCN CACHE"  val={nodeStatus.cs_entries}/>
            <Stat label="STATUS" val="ONLINE" green/>
          </div>
        )}
      </section>

      {/* Marquee */}
      <div className="marquee">
        <div className="marquee-inner">
          {MARQUEE.map((item,i) => (
            <span key={i} className="marquee-item">{item}<span className="sep"> ◈</span></span>
          ))}
        </div>
      </div>

      {/* Feature cards */}
      <section className={s.cards}>
        {FEATURES.map((f,i) => (
          <button
            key={f.id}
            data-mag={f.status==='LIVE' ? '0.1' : undefined}
            className={`${s.card} ${f.status!=='LIVE' ? s.cardDim : ''} reveal`}
            style={{animationDelay:`${i*0.07}s`}}
            onClick={() => f.status==='LIVE' && onSelect(f.id)}
            disabled={f.status!=='LIVE'}
          >
            <div className={s.cardHead}>
              <span className={s.cardIcon}>{f.icon}</span>
              <span className={`${s.cardStatus} ${f.status==='LIVE' ? s.live : s.soon}`}>{f.status}</span>
            </div>
            <div className={s.cardLabel}>{f.label}</div>
            <div className={s.cardDesc}>{f.desc}</div>
            {f.status==='LIVE' && <span className={s.cardArrow}>ENTER →</span>}
            <div className={s.cardGlow}/>
          </button>
        ))}
      </section>

      {/* Identity */}
      <section className={s.ident}>
        <div className={`${s.identBox} reveal`}>
          <span className={s.identLabel}>YOUR HANDLE</span>
          <span className={s.identName}>@{username}</span>
          <span className={s.identSub}>Generated · stored locally · never shared</span>
        </div>
      </section>

      {error && <p className={s.err}>{error}</p>}
      <Footer/>
    </div>
  )
}

function Stat({label, val, green}) {
  return (
    <div className={s.stat}>
      <span className={`${s.statV} ${green?s.statG:''}`}>{val??'—'}</span>
      <span className={s.statL}>{label}</span>
    </div>
  )
}