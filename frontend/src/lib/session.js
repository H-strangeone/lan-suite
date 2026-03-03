const KEYS = {
  AUTH:   'ls_auth',    // { token, node_id, display_name } — sessionStorage
  SCREEN: 'ls_screen',  // 'home' | 'chat' | ...            — sessionStorage
  ROOM:   'ls_room',    // 'general'                        — sessionStorage
  ROOMS:  'ls_rooms',   // ['general','dev',...]            — localStorage
}

// ── Save ─────────────────────────────────────────────────────────────────────

export function saveAuth(auth) {
  if (!auth) { sessionStorage.removeItem(KEYS.AUTH); return }
  sessionStorage.setItem(KEYS.AUTH, JSON.stringify({
    token:        auth.token,
    node_id:      auth.node_id,
    display_name: auth.display_name,
  }))
}

export function saveScreen(screen) {
  sessionStorage.setItem(KEYS.SCREEN, screen)
}

export function saveRoom(room) {
  sessionStorage.setItem(KEYS.ROOM, room)
}

export function saveRooms(rooms) {
  // Only persist custom rooms (those not in the default list).
  const DEFAULT = ['general', 'dev', 'random']
  const custom = rooms.filter(r => !DEFAULT.includes(r))
  localStorage.setItem(KEYS.ROOMS, JSON.stringify(custom))
}

export function loadAuth() {
  try {
    const raw = sessionStorage.getItem(KEYS.AUTH)
    if (!raw) return null
    const auth = JSON.parse(raw)
    // Basic sanity check — if any required field is missing, discard
    if (!auth.token || !auth.node_id) { clearAuth(); return null }
    return auth
  } catch {
    return null
  }
}

export function loadScreen() {
  return sessionStorage.getItem(KEYS.SCREEN) || 'home'
}

export function loadRoom() {
  const r = sessionStorage.getItem(KEYS.ROOM) || 'general'
  
  if (r.startsWith('room-general')) return 'general'
  if (r.startsWith('room-dev'))     return 'dev'
  if (r.startsWith('room-random'))  return 'random'
  return r
}

export function loadCustomRooms() {
  try {
    const raw = localStorage.getItem(KEYS.ROOMS)
    if (!raw) return []
    const rooms = JSON.parse(raw)
    if (!Array.isArray(rooms)) return []
    // Sanitize: only allow valid room names
    return rooms.filter(r => typeof r === 'string' && /^[a-z0-9\-_]+$/.test(r))
  } catch {
    return []
  }
}



export function clearAuth() {
  sessionStorage.removeItem(KEYS.AUTH)
  sessionStorage.removeItem(KEYS.SCREEN)
  sessionStorage.removeItem(KEYS.ROOM)
}


export function isTokenExpired(token) {
  try {
    const parts = token.split('.')
    if (parts.length !== 3) return true
    // JWT payload is base64url-encoded JSON
    const payload = JSON.parse(atob(parts[1].replace(/-/g, '+').replace(/_/g, '/')))
    if (!payload.exp) return false
    // exp is Unix timestamp in seconds
    return Date.now() / 1000 > payload.exp
  } catch {
    return true // if we can't parse it, treat as expired
  }
}