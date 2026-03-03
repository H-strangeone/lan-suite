import { useState, useEffect, useRef, useCallback } from 'react'
import wsClient, { STATE } from '../lib/ws'
import styles from './Dashboard.module.css'

const API = 'http://localhost:8080'

// ── Auth gate ─────────────────────────────────────────────────────────────────
// If no token in memory, show login form. Otherwise show the app.
function useAuth() {
  const [auth, setAuth] = useState(null) // { token, nodeID, displayName }
  const [error, setError] = useState('')

  const login = async (displayName) => {
    setError('')
    try {
      const res = await fetch(`${API}/api/auth`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ display_name: displayName, services: ['chat', 'video'] }),
      })
      if (!res.ok) throw new Error(await res.text())
      const data = await res.json()
      setAuth({ token: data.token, nodeID: data.node_id, displayName: data.display_name })
    } catch (e) {
      setError(e.message)
    }
  }

  return { auth, error, login }
}

// ── Main Dashboard ────────────────────────────────────────────────────────────
export default function Dashboard() {
  const { auth, error: authError, login } = useAuth()
  const [room, setRoom] = useState('general')

  // Connect WebSocket once we have auth
  useEffect(() => {
    if (!auth) return
    wsClient.connect(auth.token, auth.nodeID, auth.displayName, ['chat', 'video'])
    return () => wsClient.disconnect()
  }, [auth])

  if (!auth) return <LoginScreen onLogin={login} error={authError} />
  return <AppShell auth={auth} room={room} onRoomChange={setRoom} />
}

// ── Login screen ──────────────────────────────────────────────────────────────
function LoginScreen({ onLogin, error }) {
  const [name, setName] = useState('')

  const submit = (e) => {
    e.preventDefault()
    if (name.trim()) onLogin(name.trim())
  }

  return (
    <div className={styles.loginWrap}>
      <div className={styles.loginBox}>
        <pre className={styles.loginLogo}>{`
  ██╗      █████╗ ███╗   ██╗    ███████╗██╗   ██╗██╗████████╗███████╗
  ██║     ██╔══██╗████╗  ██║    ██╔════╝██║   ██║██║╚══██╔══╝██╔════╝
  ██║     ███████║██╔██╗ ██║    ███████╗██║   ██║██║   ██║   █████╗
  ██║     ██╔══██║██║╚██╗██║    ╚════██║██║   ██║██║   ██║   ██╔══╝
  ███████╗██║  ██║██║ ╚████║    ███████║╚██████╔╝██║   ██║   ███████╗hehehe
  ╚══════╝╚═╝  ╚═╝╚═╝  ╚═══╝   ╚══════╝ ╚═════╝ ╚═╝   ╚═╝   ╚══════╝`}</pre>
        <p className={styles.loginSub}>DECENTRALIZED LAN SUITE</p>
        <form onSubmit={submit} className={styles.loginForm}>
          <input
            className={styles.loginInput}
            placeholder="ENTER YOUR NAME"
            value={name}
            onChange={e => setName(e.target.value)}
            autoFocus
            maxLength={64}
          />
          <button className={styles.loginBtn} type="submit" disabled={!name.trim()}>
            CONNECT TO NODE
          </button>
        </form>
        {error && <p className={styles.loginError}>{error}</p>}
      </div>
    </div>
  )
}

// ── App shell ─────────────────────────────────────────────────────────────────
function AppShell({ auth, room, onRoomChange }) {
  const [wsState, setWsState]   = useState(STATE.CONNECTING)
  const [peers, setPeers]       = useState([])
  const [messages, setMessages] = useState([])
  const [inputText, setInput]   = useState('')
  const [rooms]                 = useState(['general', 'dev', 'random'])
  const bottomRef               = useRef(null)

  // Subscribe to WebSocket events
  useEffect(() => {
    const unsubs = [
      wsClient.on('state', ({ state }) => setWsState(state)),

      wsClient.on('peer_list', (msg) => {
        const payload = parsePayload(msg.payload)
        if (payload?.peers) setPeers(payload.peers)
      }),

      wsClient.on('peer_join', (msg) => {
        const payload = parsePayload(msg.payload)
        if (payload?.peer) {
          setPeers(prev => {
            const exists = prev.find(p => p.node_id === payload.peer.node_id)
            return exists ? prev : [...prev, payload.peer]
          })
        }
      }),

      wsClient.on('peer_left', (msg) => {
        const payload = parsePayload(msg.payload)
        if (payload?.node_id) {
          setPeers(prev => prev.filter(p => p.node_id !== payload.node_id))
        }
      }),

      wsClient.on('chat_msg', (msg) => {
        const payload = parsePayload(msg.payload)
        if (payload && msg.room === room) {
          setMessages(prev => {
            // Deduplicate by ID
            if (prev.find(m => m.id === payload.id)) return prev
            return [...prev, payload]
          })
        }
      }),

      wsClient.on('error', (msg) => {
        console.error('[ws] server error:', msg.error)
      }),
    ]

    return () => unsubs.forEach(u => u())
  }, [room])

  // Join room when connected or room changes
  useEffect(() => {
    if (wsState === STATE.CONNECTED) {
      wsClient.joinRoom(room)
      setMessages([])   // clear messages when switching rooms
      loadHistory(room) // load persisted history from HTTP
    }
  }, [wsState, room])

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const loadHistory = async (roomID) => {
    try {
      const res = await fetch(`${API}/api/chat/${roomID}/messages?limit=50`, {
        headers: { Authorization: `Bearer ${wsClient.token}` },
      })
      if (!res.ok) return
      const data = await res.json()
      if (data.messages?.length > 0) {
        setMessages(data.messages)
      }
    } catch (e) {
      console.error('Failed to load history:', e)
    }
  }

  const sendMessage = useCallback((e) => {
    e.preventDefault()
    const text = inputText.trim()
    if (!text || wsState !== STATE.CONNECTED) return
    wsClient.sendChat(text)
    setInput('')
  }, [inputText, wsState])

  const handleKey = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage(e)
    }
  }

  return (
    <div className={styles.shell}>

      {/* ── Sidebar ── */}
      <aside className={styles.sidebar}>
        <div className={styles.sidebarHeader}>
          <span className={styles.nodeName}>{auth.displayName}</span>
          <StatusDot state={wsState} />
        </div>

        <section className={styles.sideSection}>
          <p className={styles.sideLabel}>ROOMS</p>
          {rooms.map(r => (
            <button
              key={r}
              className={`${styles.roomBtn} ${r === room ? styles.roomActive : ''}`}
              onClick={() => onRoomChange(r)}
            >
              # {r}
            </button>
          ))}
        </section>

        <section className={styles.sideSection}>
          <p className={styles.sideLabel}>LAN PEERS ({peers.length})</p>
          {peers.length === 0
            ? <p className={styles.noPeers}>No peers found</p>
            : peers.map(p => (
              <div key={p.node_id} className={styles.peer}>
                <span className={styles.peerDot} />
                <span className={styles.peerName}>{p.display_name}</span>
              </div>
            ))
          }
        </section>
      </aside>

      {/* ── Main chat area ── */}
      <main className={styles.main}>
        <header className={styles.chatHeader}>
          <span className={styles.chatRoom}># {room}</span>
          <span className={styles.chatStatus}>{wsState.toUpperCase()}</span>
        </header>

        <div className={styles.messages}>
          {messages.length === 0 && (
            <div className={styles.empty}>
              No messages yet. Say something.
            </div>
          )}
          {messages.map((msg, i) => (
            <MessageBubble
              key={msg.id || i}
              msg={msg}
              isOwn={msg.from_node_id === auth.nodeID}
            />
          ))}
          <div ref={bottomRef} />
        </div>

        <form className={styles.inputRow} onSubmit={sendMessage}>
          <textarea
            className={styles.textInput}
            placeholder={wsState === STATE.CONNECTED
              ? `Message #${room}`
              : 'Connecting...'}
            value={inputText}
            onChange={e => setInput(e.target.value)}
            onKeyDown={handleKey}
            disabled={wsState !== STATE.CONNECTED}
            rows={1}
            maxLength={4000}
          />
          <button
            className={styles.sendBtn}
            type="submit"
            disabled={!inputText.trim() || wsState !== STATE.CONNECTED}
          >
            SEND
          </button>
        </form>
      </main>

    </div>
  )
}

// ── Sub-components ────────────────────────────────────────────────────────────

function MessageBubble({ msg, isOwn }) {
  const time = msg.timestamp
    ? new Date(msg.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : ''

  return (
    <div className={`${styles.msg} ${isOwn ? styles.msgOwn : ''}`}>
      <div className={styles.msgMeta}>
        <span className={styles.msgName}>{msg.from_name}</span>
        {msg.seq_no && <span className={styles.msgSeq}>#{msg.seq_no}</span>}
        <span className={styles.msgTime}>{time}</span>
      </div>
      <div className={styles.msgText}>{msg.text}</div>
    </div>
  )
}

function StatusDot({ state }) {
  const color = {
    [STATE.CONNECTED]:    '#b8ff35',
    [STATE.CONNECTING]:   '#ffcc00',
    [STATE.RECONNECTING]: '#ffcc00',
    [STATE.DISCONNECTED]: '#ff3b3b',
  }[state] || '#666'

  return (
    <span
      className={styles.statusDot}
      style={{ backgroundColor: color }}
      title={state}
    />
  )
}

// ── Helpers ────────────────────────────────────────────────────────────────────
function parsePayload(payload) {
  if (!payload) return null
  if (typeof payload === 'object') return payload
  try { return JSON.parse(payload) } catch { return null }
}