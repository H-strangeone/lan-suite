import { useState, useEffect, useRef, useCallback } from 'react'
import wsClient, { STATE } from '../lib/ws'
import { saveRoom, loadRoom } from '../lib/session'
import RoomBrowser from '../components/RoomBrowser'
import Footer from '../components/Footer'
import s from './ChatApp.module.css'

const API = 'http://localhost:8080'
const DEFAULT_ROOM_IDS = ['general', 'dev', 'random']
const DEFAULT_ROOMS = [
  { id:'general', name:'general', creator_id:'system', creator_name:'system', is_public:true },
  { id:'dev',     name:'dev',     creator_id:'system', creator_name:'system', is_public:true },
  { id:'random',  name:'random',  creator_id:'system', creator_name:'system', is_public:true },
]

export default function ChatApp({ auth, username }) {
  const [wsState,     setWsState]     = useState(STATE.CONNECTING)
  const [messages,    setMessages]    = useState([])
  const [roomList,    setRoomList]    = useState(DEFAULT_ROOMS)
  const [wsRoomList,  setWsRoomList]  = useState(null)
  const [showBrowser, setShowBrowser] = useState(false)
  const [showMembers, setShowMembers] = useState(false)
  const [input,       setInput]       = useState('')
  const [notice,      setNotice]      = useState(null)
  // Peers come from wsClient singleton — zero duplicates guaranteed
  const [peers, setPeers] = useState([])

  const savedRoom = loadRoom()
  const [activeRoomID,   setActiveRoomID]   = useState(savedRoom || 'general')
  const [activeRoomMeta, setActiveRoomMeta] = useState(
    DEFAULT_ROOMS.find(r => r.id === savedRoom) || DEFAULT_ROOMS[0]
  )

  const [confirm,  setConfirm]  = useState(null) // { msg, onYes }
  const [pwPrompt, setPwPrompt] = useState(null) // { id, name } for private rooms
  const [pwInput,  setPwInput]  = useState('')
  const disbandedRef = useRef(new Set()) // track locally disbanded room IDs
  const bottomRef   = useRef(null)
  const inputRef    = useRef(null)
  const roomIDRef   = useRef(activeRoomID)
  const needsJoinRef = useRef(true)

  useEffect(() => { roomIDRef.current = activeRoomID }, [activeRoomID])
  useEffect(() => { saveRoom(activeRoomID) }, [activeRoomID])

  // Subscribe to wsClient peer list — deduped Map, __lan__ filtered out in ws.js
  useEffect(() => {
    return wsClient.onPeers(setPeers)
  }, [])

  // WebSocket — chat + room list only
  useEffect(() => {
    if (!auth?.token) return
    wsClient.connect(auth.token, auth.node_id, username, ['chat', 'video'])

    const unsub = [
      wsClient.on('state', ({ state }) => {
        setWsState(state)
        if (state === STATE.CONNECTED) {
          wsClient.requestRoomList()
          if (needsJoinRef.current) {
            needsJoinRef.current = false
            wsClient.joinRoomByID(roomIDRef.current, '')
            loadHistory(roomIDRef.current)
          }
          inputRef.current?.focus()
        }
        if (state === STATE.RECONNECTING || state === STATE.DISCONNECTED) {
          needsJoinRef.current = true
        }
      }),
      wsClient.on('room_list', msg => {
        const list = parse(msg.payload)
        if (Array.isArray(list)) {
          // Filter out any rooms that were disbanded during this session
          const filtered = list.filter(r => !disbandedRef.current.has(r.id))
          setRoomList(filtered); setWsRoomList(filtered)
          const cur = filtered.find(r => r.id === roomIDRef.current)
          if (cur) setActiveRoomMeta(cur)
        }
      }),
      wsClient.on('chat_msg', msg => {
        const p = parse(msg.payload)
        if (p && (msg.room === roomIDRef.current || p.room === roomIDRef.current))
          setMessages(prev => prev.find(m => m.id === p.id) ? prev : [...prev, p])
      }),
      wsClient.on('kicked', msg => {
        if (msg.room === roomIDRef.current) {
          setNotice(`You were removed from #${activeRoomMeta?.name || roomIDRef.current}`)
          _joinRoom('general', '')
        }
      }),
      wsClient.on('room_disbanded', msg => {
        // Track disbanded room ID — prevents room_list broadcasts from re-adding it
        disbandedRef.current.add(msg.room)
        // Remove immediately from sidebar for all clients
        setRoomList(prev => prev.filter(r => r.id !== msg.room))
        setWsRoomList(prev => prev ? prev.filter(r => r.id !== msg.room) : prev)
        if (msg.room === roomIDRef.current) {
          setNotice(`Room #${activeRoomMeta?.name || roomIDRef.current} was disbanded`)
          _joinRoom('general', '')
        }
      }),
    ]
    return () => unsub.forEach(f => f())
  }, [auth?.token])

  useEffect(() => { bottomRef.current?.scrollIntoView({ behavior:'smooth' }) }, [messages])

  // Internal join helper (not useCallback to avoid stale closures)
  const _joinRoom = (roomID, password) => {
    const meta = roomList.find(r => r.id === roomID)
              || DEFAULT_ROOMS.find(r => r.id === roomID)
              || { id:roomID, name:roomID, creator_id:'unknown', is_public:true }
    setActiveRoomID(roomID); setActiveRoomMeta(meta)
    roomIDRef.current = roomID
    setMessages([])
    wsClient.clearPeers()
    if (wsClient.state === STATE.CONNECTED) {
      needsJoinRef.current = false
      wsClient.joinRoomByID(roomID, password)
      loadHistory(roomID)
    } else {
      needsJoinRef.current = true
    }
    setShowBrowser(false)
    setShowMembers(false)
  }

  const joinRoom = useCallback(_joinRoom, [roomList])

  const handleBrowserAction = useCallback((roomID, extra) => {
    if (roomID === '__create__') {
      const createdName = extra.name
      const unsubOnce = wsClient.on('room_list', msg => {
        const list = parse(msg.payload)
        if (!Array.isArray(list)) return
        const newRoom = list.find(r => r.name === createdName && r.creator_id === auth.node_id)
        if (!newRoom) return
        unsubOnce()
        setActiveRoomID(newRoom.id); setActiveRoomMeta(newRoom)
        roomIDRef.current = newRoom.id
        setMessages([]); wsClient.clearPeers()
        loadHistory(newRoom.id)
      })
      wsClient.createRoom(createdName, extra.password || '')
      setShowBrowser(false)
    } else {
      joinRoom(roomID, typeof extra === 'string' ? extra : '')
    }
  }, [joinRoom, auth?.node_id])

  const loadHistory = async (roomID) => {
    try {
      const res = await fetch(`${API}/api/chat/${roomID}/messages?limit=100`, {
        headers: { Authorization: `Bearer ${auth.token}` }
      })
      if (!res.ok) return
      const d = await res.json()
      if (d.messages?.length) setMessages(d.messages)
    } catch {}
  }

  const send = useCallback(() => {
    const t = input.trim()
    if (!t || wsState !== STATE.CONNECTED) return
    wsClient.sendChat(t); setInput('')
    inputRef.current?.focus()
  }, [input, wsState])

  const connected   = wsState === STATE.CONNECTED
  const isPrivate   = activeRoomMeta && !activeRoomMeta.is_public
  const isOwner     = activeRoomMeta?.creator_id === auth.node_id
  const isDefault   = DEFAULT_ROOM_IDS.includes(activeRoomID)
  const customRooms = roomList.filter(r => !DEFAULT_ROOM_IDS.includes(r.id))

  return (
    <div className={s.layout}>
      <aside className={s.roombar}>
        <div className={s.rbHead}>
          <span className={s.rbTitle}>ROOMS</span>
          <button className={s.browseBtn} onClick={() => setShowBrowser(true)} title="Browse & create rooms">⊞</button>
        </div>

        <div className={s.rbSection}>DEFAULT</div>
        {DEFAULT_ROOMS.map(r => (
          <button key={r.id}
            className={`${s.roomBtn} ${r.id === activeRoomID ? s.roomActive : ''}`}
            onClick={() => joinRoom(r.id, '')}>
            <span className={s.hash}>#</span>{r.name}
            {r.id === activeRoomID && <span className={s.roomPip} />}
          </button>
        ))}

        {customRooms.length > 0 && (
          <>
            <div className={s.rbSection}>CUSTOM</div>
            {customRooms.map(r => (
              <button key={r.id}
                className={`${s.roomBtn} ${r.id === activeRoomID ? s.roomActive : ''}`}
                onClick={() => r.is_public ? joinRoom(r.id,'') : (setPwPrompt({id:r.id,name:r.name}), setPwInput(''))}>
                <span className={s.hash}>#</span>{r.name}
                {!r.is_public && <span className={s.lockPip}> (priv)</span>}
                {r.id === activeRoomID && <span className={s.roomPip} />}
              </button>
            ))}
          </>
        )}
        <br></br>
        <div style={{ display: 'flex', justifyContent: 'center', padding: '0.6rem 0' }}><button className={s.offsetBtn} onClick={() => setShowBrowser(true)}>NEW ROOM</button></div>

        <div className={s.rbSection}>MEMBERS ({peers.length + 1})</div>
        <div className={`${s.peer} ${s.peerSelf}`}>
          <span className={s.peerDot} style={{background:'#60a5fa',boxShadow:'0 0 5px #60a5fa'}} />
          <span className={s.peerName}>{username}</span>
          <span className={s.youTag}>YOU</span>
        </div>
        {peers.map(p => (
          <div key={p.node_id} className={s.peer}>
            <span className={s.peerDot} />
            <span className={s.peerName}>@{p.display_name}</span>
          </div>
        ))}
        {peers.length === 0 && (
          <p style={{padding:'0.3rem 0.75rem',fontSize:'0.6rem',color:'var(--faint)',margin:0}}>No one else here</p>
        )}
      </aside>

      <main className={s.chat}>
        <header className={s.chatHead}>
          <div className={s.chatHeadLeft}>
            <span className={s.chatRoom}># {activeRoomMeta?.name || activeRoomID}</span>
            {isPrivate && <span className={s.privateBadge}>Private</span>}
            {!isDefault && activeRoomMeta?.creator_name && (
              <span className={s.createdBy}>by @{activeRoomMeta.creator_name}</span>
            )}
          </div>
          <div style={{display:'flex',alignItems:'center',gap:'0.5rem'}}>
            {isPrivate && (
              <button className={s.offsetBtn} onClick={() => setShowMembers(true)} style={{color:'white'}}>MEMBERS</button>
            )}
            {!isDefault && !isOwner && (
              <button className={`${s.offsetBtn} ${s.offsetDanger}`}
                onClick={() => setConfirm({ msg: `Leave #${activeRoomMeta?.name || activeRoomID}?`, onYes: () => { wsClient.leaveRoom(); joinRoom('general','') } })}
                style={{color:'#ff4444'}}>LEAVE</button>
            )}
            <span className={`${s.connState} ${s['cs_'+wsState]}`}>{wsState.toUpperCase()}</span>
          </div>
        </header>

        {notice && (
          <div style={{padding:'0.4rem 1rem',background:'rgba(255,68,68,0.08)',color:'#ff4444',fontSize:'0.68rem',borderBottom:'1px solid rgba(255,68,68,0.2)',display:'flex',justifyContent:'space-between'}}>
            <span>{notice}</span>
            <button onClick={() => setNotice(null)} style={{background:'none',border:'none',color:'#ff4444',cursor:'pointer'}}>✕</button>
          </div>
        )}

        <div className={s.msgs}>
          {messages.length === 0 && (
            <div className={s.empty}>
              {connected ? `No messages in #${activeRoomMeta?.name||activeRoomID} yet.` : 'Connecting…'}
            </div>
          )}
          {messages.map((m, i) => (
            <Bubble key={m.id||i} msg={m} own={m.from_node_id===auth.node_id} prev={messages[i-1]} />
          ))}
          <div ref={bottomRef} />
        </div>

        <div className={s.inputRow}>
          <textarea ref={inputRef} className={s.input} rows={1} maxLength={4000}
            placeholder={connected ? `Message #${activeRoomMeta?.name||activeRoomID}  —  Enter to send` : wsState+'…'}
            value={input} onChange={e => setInput(e.target.value)} disabled={!connected}
            onKeyDown={e => { if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();send()} }}
          />
          <button className={s.sendBtn} onClick={send} disabled={!input.trim()||!connected}>SEND</button>
        </div>

        <Footer />
      </main>

      {showBrowser && (
        <RoomBrowser auth={auth} currentRoomID={activeRoomID}
          onJoin={handleBrowserAction} onClose={() => setShowBrowser(false)} wsRoomList={wsRoomList} />
      )}

      {showMembers && activeRoomMeta && (
        <MemberPanel room={activeRoomMeta} auth={auth} peers={peers} isOwner={isOwner}
          onKick={nid => wsClient.kickMember(nid)}
          onLeave={() => setConfirm({ msg: `Leave #${activeRoomMeta?.name || activeRoomID}?`, onYes: () => { wsClient.leaveRoom(); joinRoom('general',''); setShowMembers(false) } })}
          onDisband={() => setConfirm({ msg: `Disband #${activeRoomMeta?.name || activeRoomID}? This cannot be undone.`, onYes: () => { wsClient.disbandRoom(); joinRoom('general',''); setShowMembers(false) } })}
          onClose={() => setShowMembers(false)} />
      )}
      {/* Confirm dialog */}
      {confirm && (
        <div style={dlg.overlay} onClick={e => e.target===e.currentTarget && setConfirm(null)}>
          <div style={dlg.panel}>
            <p style={dlg.msg}>{confirm.msg}</p>
            <div style={dlg.btns}>
              <button style={dlg.cancel} onClick={() => setConfirm(null)}>CANCEL</button>
              <button style={dlg.yes} onClick={() => { confirm.onYes(); setConfirm(null) }}>YES</button>
            </div>
          </div>
        </div>
      )}

      {/* Private room password prompt */}
      {pwPrompt && (
        <div style={dlg.overlay} onClick={e => e.target===e.currentTarget && setPwPrompt(null)}>
          <div style={dlg.panel}>
            <p style={dlg.msg}>Password for <strong style={{color:'var(--green)'}}>#{pwPrompt.name}</strong></p>
            <input autoFocus type="password" value={pwInput}
              onChange={e => setPwInput(e.target.value)}
              onKeyDown={e => {
                if (e.key==='Enter') { joinRoom(pwPrompt.id, pwInput); setPwPrompt(null) }
                if (e.key==='Escape') setPwPrompt(null)
              }}
              placeholder="enter password"
              style={dlg.pwInput}
            />
            <div style={dlg.btns}>
              <button style={dlg.cancel} onClick={() => setPwPrompt(null)}>CANCEL</button>
              <button style={dlg.yes} onClick={() => { joinRoom(pwPrompt.id, pwInput); setPwPrompt(null) }}>JOIN</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function MemberPanel({ room, auth, peers, isOwner, onKick, onLeave, onDisband, onClose }) {
  return (
    <div style={mp.overlay} onClick={e => e.target===e.currentTarget && onClose()}>
      <div style={mp.panel}>
        <div style={mp.head}>
          <span style={mp.title}>#{room.name} · MEMBERS</span>
          <button style={mp.close} onClick={onClose}>✕</button>
        </div>
        {isOwner && <div style={mp.ownerTag}> ROOM OWNER</div>}
        <div style={mp.list}>
          <div style={mp.row}>
            <span style={{...mp.dot,background:'#60a5fa',boxShadow:'0 0 5px #60a5fa'}} />
            <span style={mp.name}>@{auth.display_name || 'you'}</span>
            <span style={mp.you}>YOU</span>
          </div>
          {peers.map(p => (
            <div key={p.node_id} style={mp.row}>
              <span style={mp.dot} />
              <span style={mp.name}>@{p.display_name}</span>
              {isOwner && <button style={mp.kick} onClick={() => onKick(p.node_id)}>KICK</button>}
            </div>
          ))}
          {peers.length === 0 && <p style={{padding:'0.5rem 1rem',fontSize:'0.65rem',color:'var(--faint)',margin:0}}>No other members online</p>}
        </div>
        <div style={mp.foot}>
          {isOwner
            ? <button className={s.roomleavekaro} onClick={onDisband}>DISBAND ROOM</button>
            : <button className={s.roomleavekaro} onClick={onLeave}>LEAVE ROOM</button>
          }
        </div>
      </div>
    </div>
  )
}

const mp = {
  overlay: {position:'fixed',inset:0,zIndex:200,background:'rgba(0,0,0,0.72)',display:'flex',alignItems:'center',justifyContent:'center'},
  panel:   {background:'var(--bg1)',border:'1px solid var(--border)',width:310,maxHeight:'65vh',display:'flex',flexDirection:'column',fontFamily:'var(--mono)'},
  head:    {display:'flex',alignItems:'center',justifyContent:'space-between',padding:'0.75rem 1rem',borderBottom:'1px solid var(--border)'},
  title:   {fontSize:'0.65rem',letterSpacing:'0.15em',color:'var(--text)'},
  close:   {background:'none',border:'none',color:'var(--dim)',fontSize:'1rem',cursor:'pointer'},
  ownerTag:{padding:'0.35rem 1rem',fontSize:'0.58rem',color:'var(--green)',letterSpacing:'0.1em',borderBottom:'1px solid var(--border)'},
  list:    {flex:1,overflowY:'auto',padding:'0.5rem 0'},
  row:     {display:'flex',alignItems:'center',gap:'0.5rem',padding:'0.4rem 1rem'},
  dot:     {width:6,height:6,borderRadius:'50%',background:'var(--green)',boxShadow:'0 0 5px var(--green)',flexShrink:0},
  name:    {flex:1,fontSize:'0.72rem',color:'var(--text)'},
  you:     {fontSize:'0.52rem',color:'#60a5fa',letterSpacing:'0.1em'},
  kick:    {background:'none',border:'1px solid rgba(255,68,68,0.3)',color:'#ff4444',fontSize:'0.52rem',padding:'0.15rem 0.4rem',fontFamily:'var(--mono)',cursor:'pointer'},
  foot:    {padding:'0.75rem 1rem',borderTop:'1px solid var(--border)'},
  btn:     {width:'100%',background:'none',border:'1px solid',fontFamily:'var(--mono)',fontSize:'0.65rem',letterSpacing:'0.1em',padding:'0.5rem',cursor:'pointer'},
}

const dlg = {
  overlay: {position:'fixed',inset:0,zIndex:500,background:'rgba(0,0,0,0.82)',display:'flex',alignItems:'center',justifyContent:'center',fontFamily:'var(--mono)'},
  panel:   {background:'var(--bg1)',border:'1px solid var(--border)',padding:'1.5rem 2rem',minWidth:280,maxWidth:360,display:'flex',flexDirection:'column',gap:'1rem'},
  msg:     {margin:0,fontSize:'0.75rem',color:'var(--text)',letterSpacing:'0.05em',lineHeight:1.6},
  btns:    {display:'flex',gap:'0.75rem',justifyContent:'flex-end'},
  cancel:  {background:'none',border:'1px solid var(--border)',color:'var(--dim)',fontFamily:'var(--mono)',fontSize:'0.65rem',letterSpacing:'0.1em',padding:'0.4rem 1.2rem',cursor:'pointer'},
  yes:     {background:'none',border:'1px solid rgba(255,68,68,0.5)',color:'#ff4444',fontFamily:'var(--mono)',fontSize:'0.65rem',letterSpacing:'0.1em',padding:'0.4rem 1.2rem',cursor:'pointer'},
  pwInput: {background:'rgba(0,0,0,0.4)',border:'1px solid var(--border)',color:'var(--text)',fontFamily:'var(--mono)',fontSize:'0.72rem',padding:'0.5rem 0.75rem',width:'100%',outline:'none',boxSizing:'border-box'},
}

function Bubble({ msg, own, prev }) {
  const grouped = prev?.from_node_id === msg.from_node_id
  const time = msg.timestamp
    ? new Date(msg.timestamp).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'})
    : ''
  return (
    <div className={`${s.msg} ${own?s.msgOwn:''} ${grouped?s.msgGroup:''}`}>
      {!grouped && (
        <div className={s.msgMeta}>
          <span className={`${s.msgName} ${own?s.nameOwn:''}`}>@{msg.from_name}</span>
          {msg.seq_no && <span className={s.seq}>#{msg.seq_no}</span>}
          <span className={s.time}>{time}</span>
        </div>
      )}
      <div className={s.bubble}>{msg.text}</div>
    </div>
  )
}

const parse = p => {
  if (!p) return null
  if (typeof p === 'object') return p
  try { return JSON.parse(p) } catch { return null }
}