import { useState, useEffect, useRef, useCallback } from 'react'
import s from './RoomBrowser.module.css'

const API = 'http://localhost:8080'

/*
  RoomBrowser — modal/panel for searching, creating, and joining rooms.

  Props:
    auth          — { token, node_id, display_name }
    currentRoomID — string, currently joined room ID
    onJoin        — fn(roomID, password) — called when user joins a room
    onClose       — fn() — close the browser panel
    wsRoomList    — array of RoomInfo from live WS push (may be null initially)
*/
export default function RoomBrowser({ auth, currentRoomID, onJoin, onClose, wsRoomList }) {
  const [rooms, setRooms]         = useState([])
  const [search, setSearch]       = useState('')
  const [creating, setCreating]   = useState(false)
  const [newName, setNewName]     = useState('')
  const [newPass, setNewPass]     = useState('')
  const [showPass, setShowPass]   = useState(false) // toggle visibility
  const [genPass, setGenPass]     = useState('')     // generated password display
  const [joinTarget, setJoinTarget] = useState(null) // { id, name } needing password
  const [joinPass, setJoinPass]   = useState('')
  const [error, setError]         = useState('')
  const [loading, setLoading]     = useState(false)
  const searchRef = useRef(null)
  const nameRef   = useRef(null)

  // ── Load rooms on mount + when wsRoomList changes ─────────────────────────
  useEffect(() => {
    if (wsRoomList) {
      setRooms(wsRoomList)
    } else {
      fetchRooms()
    }
  }, [wsRoomList])

  useEffect(() => { searchRef.current?.focus() }, [])

  const fetchRooms = async () => {
    try {
      const res = await fetch(`${API}/api/rooms`, {
        headers: { Authorization: `Bearer ${auth.token}` }
      })
      if (res.ok) setRooms(await res.json())
    } catch {}
  }

  // ── Password generation ───────────────────────────────────────────────────
  const generatePassword = () => {
    const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$'
    const arr = new Uint8Array(16)
    crypto.getRandomValues(arr)
    const pass = Array.from(arr).map(b => chars[b % chars.length]).join('')
    setNewPass(pass)
    setGenPass(pass)
    setShowPass(true)
  }

  // ── Create room ───────────────────────────────────────────────────────────
  // Room creation goes through WebSocket (not HTTP) so all peers get instant push
  const handleCreate = useCallback(() => {
    const name = newName.trim().toLowerCase().replace(/[^a-z0-9\-_]/g, '-').replace(/^-+|-+$/g, '')
    if (!name) { setError('Enter a room name'); return }
    setError('')
    // Emit create_room via ws — parent passes the ws client through onJoin callback
    // We signal "create" by calling onJoin with a special sentinel
    onJoin('__create__', { name, password: newPass })
    setCreating(false)
    setNewName('')
    setNewPass('')
    setGenPass('')
  }, [newName, newPass, onJoin])

  // ── Join room ─────────────────────────────────────────────────────────────
  const handleJoin = (room) => {
    if (room.id === currentRoomID) { onClose(); return }
    if (room.is_public) {
      onJoin(room.id, '')
    } else {
      setJoinTarget(room)
      setJoinPass('')
      setError('')
    }
  }

  const handleJoinWithPass = () => {
    if (!joinPass.trim()) { setError('Enter the room password'); return }
    onJoin(joinTarget.id, joinPass)
    setJoinTarget(null)
    setJoinPass('')
    setError('')
  }

  // ── Filter ────────────────────────────────────────────────────────────────
  const filtered = rooms.filter(r => {
    if (!search) return true
    const q = search.toLowerCase()
    return r.name.toLowerCase().includes(q) || r.creator_name?.toLowerCase().includes(q)
  })

  // ── Render ────────────────────────────────────────────────────────────────
  return (
    <div className={s.overlay} onClick={e => e.target === e.currentTarget && onClose()}>
      <div className={s.panel}>

        {/* Header */}
        <div className={s.head}>
          <span className={s.title}>ROOMS</span>
          <button className={s.closeBtn} onClick={onClose} aria-label="close">✕</button>
        </div>

        {/* Search bar */}
        <div className={s.searchRow}>
          <input
            ref={searchRef}
            className={s.searchInput}
            placeholder="Search rooms or creators..."
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
          <button
            className={s.createBtn}
            onClick={() => { setCreating(v => !v); setTimeout(() => nameRef.current?.focus(), 50) }}
          >
            {creating ? 'CANCEL' : '+ NEW'}
          </button>
        </div>

        {/* Create room form */}
        {creating && (
          <div className={s.createForm}>
            <div className={s.createRow}>
              <input
                ref={nameRef}
                className={s.createInput}
                placeholder="room-name (a-z 0-9 - _)"
                value={newName}
                onChange={e => setNewName(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleCreate()}
                maxLength={32}
              />
            </div>
            <div className={s.passRow}>
              <input
                className={s.createInput}
                type={showPass ? 'text' : 'password'}
                placeholder="Password (leave empty for public room)"
                value={newPass}
                onChange={e => { setNewPass(e.target.value); setGenPass('') }}
                maxLength={128}
              />
              <button className={s.iconBtn} onClick={() => setShowPass(v => !v)} title="Show/hide">
                {showPass ? '>:<' :'👁'}
              </button>
              <button className={s.iconBtn} onClick={generatePassword} title="Generate strong password">
                GEN
              </button>
            </div>
            {genPass && (
              <div className={s.genPassDisplay}>
                <span className={s.genLabel}>GENERATED PASSWORD — COPY NOW:</span>
                <code className={s.genCode}>{genPass}</code>
                <span className={s.genNote}>Share this with people you want to invite. It will NOT be shown again.</span>
              </div>
            )}
            {error && <p className={s.error}>{error}</p>}
            <div className={s.createActions}>
              <span className={s.createHint}>
                {newPass ? ' Password-protected' : ' Public room'}
              </span>
              <button className={s.createSubmit} onClick={handleCreate}>
                CREATE ROOM
              </button>
            </div>
          </div>
        )}

        {/* Password prompt for joining private rooms */}
        {joinTarget && (
          <div className={s.joinPrompt}>
            <span className={s.joinPromptLabel}>
              wow <strong>#{joinTarget.name}</strong> is password-protected
            </span>
            <div className={s.joinPassRow}>
              <input
                className={s.createInput}
                type="password"
                placeholder="Enter room password"
                value={joinPass}
                onChange={e => setJoinPass(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleJoinWithPass()}
                autoFocus
              />
              <button className={s.joinPassBtn} onClick={handleJoinWithPass}>JOIN</button>
              <button className={s.cancelBtn} onClick={() => setJoinTarget(null)}>CANCEL</button>
            </div>
            {error && <p className={s.error}>{error}</p>}
          </div>
        )}

        {/* Room list */}
        <div className={s.list}>
          {filtered.length === 0 && (
            <div className={s.empty}>
              {search ? `No rooms matching "${search}"` : 'No rooms yet. Create one!'}
            </div>
          )}
          {filtered.map(room => (
            <RoomRow
              key={room.id}
              room={room}
              active={room.id === currentRoomID}
              onJoin={() => handleJoin(room)}
            />
          ))}
        </div>

        <div className={s.foot}>
          <span>{rooms.length} room{rooms.length !== 1 ? 's' : ''}</span>
          <span>{filtered.length !== rooms.length ? `${filtered.length} matching` : ''}</span>
        </div>
      </div>
    </div>
  )
}

function RoomRow({ room, active, onJoin }) {
  const isSystem = room.creator_id === 'system'
  return (
    <div className={`${s.roomRow} ${active ? s.roomActive : ''}`}>
      <div className={s.roomInfo}>
        <div className={s.roomNameRow}>
          <span className={s.roomHash}>#</span>
          <span className={s.roomName}>{room.name}</span>
          {!room.is_public && <span className={s.lockIcon} title="Password protected">🔒</span>}
        </div>
        <div className={s.roomMeta}>
          {isSystem
            ? <span className={s.systemBadge}>DEFAULT</span>
            : <span className={s.creator}>by @{room.creator_name}</span>
          }
          {room.member_count > 0 && (
            <span className={s.members}>· {room.member_count} online</span>
          )}
        </div>
      </div>
      <button
        className={`${s.joinBtn} ${active ? s.joinBtnActive : ''}`}
        onClick={onJoin}
        disabled={active}
      >
        {active ? 'CURRENT' : 'JOIN'}
      </button>
    </div>
  )
}