import { useState, useEffect, useRef } from 'react'
import styles from './Landing.module.css'

/*
  CONCEPT: Props
  ───────────────
  Props (properties) are how a parent passes data/callbacks to a child.
  Here: App passes onEnter (a function) to Landing.
  Landing calls onEnter() when the user clicks "Enter Network".
  This is the React data flow pattern: data down, events up.

  CONCEPT: useRef
  ────────────────
  useRef creates a mutable object { current: value } that DOESN'T
  trigger re-renders when changed. Two main uses:
  1. DOM reference: const ref = useRef(); <div ref={ref}> → ref.current is the DOM node
  2. Mutable value: like an instance variable that persists across renders
  Here we use it for the IntersectionObserver — we don't want re-renders when it changes.
*/

// Features data — separating data from JSX is cleaner and easier to maintain
const FEATURES = [
  {
    icon: '⬡',
    title: 'Peer Discovery',
    desc: 'UDP multicast broadcasts locate all nodes on the LAN. No DNS. No central registry. Every peer announces itself.',
    tag: 'UDP · Multicast',
  },
  {
    icon: '◈',
    title: 'P2P Chat',
    desc: 'Messages are CCN-named content. Stored locally, cached by peers, replayed on reconnect. Zero message servers.',
    tag: 'CCN · QUIC',
  },
  {
    icon: '◉',
    title: 'Video Calls',
    desc: 'WebRTC direct peer connection. Signaling via CCN packets — no dedicated signaling server needed.',
    tag: 'WebRTC · ICE · SDP',
  },
  {
    icon: '▦',
    title: 'File Transfer',
    desc: 'Files are chunked, hashed, and addressed by content. Multiple peers serve chunks. BitTorrent-inspired, LAN-fast.',
    tag: 'Chunking · Merkle',
  },
  {
    icon: '◫',
    title: 'Distributed Drive',
    desc: 'A shared namespace. Files announced, cached, replicated across peers. No NAS, no cloud storage.',
    tag: 'DHT · CAS',
  },
  {
    icon: '◻',
    title: 'LAN Mail',
    desc: 'SMTP-inspired local agent. Mailboxes as content namespaces. Async delivery when peers come online.',
    tag: 'Async · SMTP-like',
  },
]

const PHASES = [
  { num: '1', title: 'Foundation', status: 'active', items: ['Peer discovery', 'CCN packets', 'Go scaffold', 'Frontend'] },
  { num: '2', title: 'Signaling',  status: 'next',   items: ['WebSocket rooms', 'SDP relay', 'ICE exchange', 'JWT auth'] },
  { num: '3', title: 'P2P Chat',   status: 'queue',  items: ['Named messages', 'Offline delivery', 'Rooms', 'Cache'] },
  { num: '4', title: 'Video + Files', status: 'queue', items: ['WebRTC video', 'Chunking', 'Hash verify', 'Resume'] },
  { num: '5', title: 'Drive + Mail',  status: 'future', items: ['Namespace sync', 'DHT routing', 'LAN SMTP', 'Async'] },
]

const STACK = ['Go 1.22+', 'gorilla/websocket', 'quic-go', 'React 18', 'Vite 6', 'WebRTC API', 'UDP Multicast', 'JWT', 'bcrypt', 'Zod']

export default function Landing({ onEnter }) {
  const [peerCount, setPeerCount] = useState(0)
  const [time, setTime] = useState('')
  const [nodeId] = useState(generateNodeId)  // useState(fn) — initializer runs ONCE
  const tickerRef = useRef(null)

  // Live clock
  useEffect(() => {
    const tick = () => setTime(new Date().toTimeString().slice(0, 8))
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [])

  // Fake peer count — in Phase 2, this comes from WebSocket
  useEffect(() => {
    const simulate = () => setPeerCount(Math.floor(Math.random() * 4) + 1)
    simulate()
    const id = setInterval(simulate, 6000)
    return () => clearInterval(id)
  }, [])

  /*
    CONCEPT: IntersectionObserver
    ──────────────────────────────
    Browser API that fires a callback when an element enters/exits the viewport.
    We use it for scroll-reveal animations — cards fade in as you scroll to them.
    Much more performant than scroll event listeners.
  */
  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach(entry => {
          if (entry.isIntersecting) {
            entry.target.classList.add(styles.revealed)
            // Unobserve after reveal — no need to keep watching
            observer.unobserve(entry.target)
          }
        })
      },
      { threshold: 0.1 }
    )

    // Observe all elements with the .reveal class
    document.querySelectorAll('[data-reveal]').forEach(el => observer.observe(el))

    return () => observer.disconnect()
  }, [])

  return (
    <div className={styles.page}>

      {/* ── TOP BAR ── */}
      <nav className={styles.topbar}>
        <div className={styles.topbarLogo}>LAN SUITE</div>
        <div className={styles.topbarStatus}>
          <span className={styles.statusDot}>
            <span className={styles.dot} />
            NODE ACTIVE
          </span>
          <span>{peerCount} PEER{peerCount !== 1 ? 'S' : ''}</span>
          <span>{time}</span>
        </div>
      </nav>

      {/* ── HERO ── */}
      <section className={styles.hero}>
        <div className={styles.heroGrid} />

        <p className={styles.eyebrow}>// Decentralized LAN Communication Suite</p>

        {/*
          data-text is used by CSS for the glitch effect.
          The ::before and ::after pseudo-elements read this attr()
          to duplicate the text at a slight offset.
        */}
        <h1 className={styles.headline} data-text="NO CLOUD. NO SERVERS. NO MERCY.">
          <span className={styles.lineAccent}>NO CLOUD.</span>
          <br />
          <span>NO SERVERS.</span>
          <br />
          <span className={styles.lineRed}>NO MERCY.</span>
        </h1>

        <p className={styles.sub}>
          A <strong>fully peer-to-peer</strong> communication platform for LANs.{' '}
          <strong>Chat, video, files, and distributed storage</strong> — running
          without a single central server. Content-addressed. Encrypted. Decentralized.
        </p>

        <div className={styles.actions}>
          <button className={styles.btnPrimary} onClick={onEnter}>
            → Enter Network
          </button>
          <a href="#architecture" className={styles.btnGhost}>
            $ View Architecture
          </a>
        </div>

        {/* Node ID display */}
        <div className={styles.heroNodeId}>
          <span className={styles.heroNodeLabel}>THIS NODE //</span> {nodeId}
        </div>
      </section>

      {/* ── TICKER ── */}
      <div className={styles.tickerWrap}>
        <div className={styles.ticker}>
          {/* Doubled content so the ticker loops seamlessly */}
          {[...Array(2)].map((_, i) => (
            <span key={i} className={styles.tickerInner}>
              <em>P2P CHAT</em> · WEBRTC VIDEO · FILE CHUNKING · DISTRIBUTED DRIVE ·
              CCN ROUTING · UDP DISCOVERY · QUIC TRANSPORT · NO TRACKING ·
              ZERO TELEMETRY · ZERO CLOUD · <em>OPEN NETWORK</em> ·&nbsp;
            </span>
          ))}
        </div>
      </div>

      {/* ── FEATURES ── */}
      <section className={styles.section} id="features">
        <div className={styles.sectionLabel}>// Core Features</div>
        <div className={styles.featuresGrid}>
          {FEATURES.map((f, i) => (
            <div
              key={f.title}
              className={styles.featureCard}
              data-reveal
              style={{ transitionDelay: `${i * 60}ms` }}
            >
              <div className={styles.featureIcon}>{f.icon}</div>
              <h3 className={styles.featureTitle}>{f.title}</h3>
              <p className={styles.featureDesc}>{f.desc}</p>
              <span className={styles.featureTag}>{f.tag}</span>
            </div>
          ))}
        </div>
      </section>

      {/* ── ARCHITECTURE ── */}
      <section className={styles.section} id="architecture">
        <div className={styles.sectionLabel}>// System Architecture</div>
        <div className={styles.archDiagram} data-reveal>
          <pre className={styles.archPre}>{ARCH_ART}</pre>
        </div>

        <div className={styles.stackRow}>
          {STACK.map(s => (
            <span key={s} className={styles.stackBadge}>{s}</span>
          ))}
        </div>
      </section>

      {/* ── BUILD PHASES ── */}
      <section className={styles.section}>
        <div className={styles.sectionLabel}>// Build Roadmap</div>
        <div className={styles.phases}>
          {PHASES.map((p, i) => (
            <div
              key={p.num}
              className={`${styles.phaseCard} ${styles[`phase_${p.status}`]}`}
              data-reveal
              style={{ transitionDelay: `${i * 80}ms` }}
            >
              <span className={styles.phaseNum}>{p.num}</span>
              <div className={`${styles.phaseStatus} ${styles[`status_${p.status}`]}`}>
                {p.status === 'active' ? 'CURRENT' : p.status === 'next' ? 'NEXT' : p.status.toUpperCase()}
              </div>
              <div className={styles.phaseTitle}>{p.title}</div>
              <ul className={styles.phaseItems}>
                {p.items.map(item => <li key={item}>{item}</li>)}
              </ul>
            </div>
          ))}
        </div>
      </section>

      {/* ── FOOTER ── */}
      <footer className={styles.footer}>
        <span>LAN SUITE — <span className={styles.footerAccent}>PHASE 1 / ACTIVE</span></span>
        <span>DECENTRALIZED · ENCRYPTED · LOCAL</span>
        <span>NODE: {nodeId}</span>
      </footer>

    </div>
  )
}

function generateNodeId() {
  return Array.from(crypto.getRandomValues(new Uint8Array(6)))
    .map(b => b.toString(16).padStart(2, '0').toUpperCase())
    .join(':')
}

// Architecture ASCII art — defined as a constant outside the component
// so it doesn't re-create on every render
const ARCH_ART = `
┌─────────────────────────────────────────────────────┐
│              FRONTEND  (React / Vite)                │
│    Chat UI  │  Video UI  │  Files UI  │  Drive UI   │
└──────────────────────┬──────────────────────────────┘
                       │  WebSocket + HTTP/QUIC
┌──────────────────────▼──────────────────────────────┐
│            APPLICATION LAYER  (Go)                   │
│  /chat   /video   /file   /drive   /identity        │
└──────────────────────┬──────────────────────────────┘
                       │  Named Interest/Data Packets
┌──────────────────────▼──────────────────────────────┐
│             CCN ROUTING LAYER  (Go)                  │
│   Interest Table  │  Content Store  │  FIB / PIT    │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│          TRANSPORT & DISCOVERY  (Go)                 │
│   UDP Multicast  │  TCP  │  QUIC  │  WebRTC (P2P)  │
└─────────────────────────────────────────────────────┘`.trim()