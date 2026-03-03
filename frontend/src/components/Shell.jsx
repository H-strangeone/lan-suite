import { useState } from 'react'
import s from './Shell.module.css'

const NAV = [
  { id: 'home',  icon: '⌂', label: 'HOME'  },
  { id: 'chat',  icon: '◈', label: 'CHAT'  },
  { id: 'video', icon: '◉', label: 'VIDEO' },
  { id: 'files', icon: '◫', label: 'FILES', soon: true },
  { id: 'drive', icon: '◧', label: 'DRIVE', soon: true },
]

export default function Shell({ children, active, onNav, nodeID }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <div className={s.shell}>
      {/* Sidebar */}
      <aside
        className={`${s.sidebar} ${expanded ? s.expanded : ''}`}
        onMouseEnter={() => setExpanded(true)}
        onMouseLeave={() => setExpanded(false)}
      >
        <div className={s.logo}>
          <span className={s.logoIcon}>◈</span>
          {expanded && <span className={s.logoText}>LAN SUITE</span>}
        </div>

        <nav className={s.nav}>
          {NAV.map(item => (
            <button
              key={item.id}
              data-mag="0.25"
              className={`${s.navBtn} ${active === item.id ? s.navActive : ''} ${item.soon ? s.navSoon : ''}`}
              onClick={() => !item.soon && onNav(item.id)}
              title={item.label}
            >
              <span className={s.navIcon}>{item.icon}</span>
              {expanded && (
                <span className={s.navLabel}>
                  {item.label}
                  {item.soon && <span className={s.soonTag}>SOON</span>}
                </span>
              )}
              {active === item.id && <span className={s.navPip} />}
            </button>
          ))}
        </nav>

        <div className={s.sideBottom}>
          {nodeID && (
            <div className={s.nodeId} title={nodeID}>
              <span className={s.nodeOnline} />
              {expanded && <span className={s.nodeText}>{nodeID.slice(0,8)}</span>}
            </div>
          )}
        </div>
      </aside>

      {/* Content */}
      <div className={s.content}>
        {children}
      </div>
    </div>
  )
}