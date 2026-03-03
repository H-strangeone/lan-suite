import { useEffect, useRef, useState } from 'react'
import s from './PageTransition.module.css'

export default function PageTransition({ active, onDone, children }) {
  const [phase, setPhase]     = useState('idle') // idle | enter | spin | exit | done
  const [visible, setVisible] = useState(false)
  const timerRef = useRef([])

  const clear = () => timerRef.current.forEach(clearTimeout)

  useEffect(() => {
    if (!active) return
    clear()
    setVisible(true)
    setPhase('enter')

    const t1 = setTimeout(() => setPhase('spin'),  320)
    const t2 = setTimeout(() => setPhase('exit'),  720)
    const t3 = setTimeout(() => setPhase('done'),  1050)
    const t4 = setTimeout(() => {
      setVisible(false)
      setPhase('idle')
      onDone?.()
    }, 1250)

    timerRef.current = [t1, t2, t3, t4]
    return clear
  }, [active])

  return (
    <>
      {/* Page content — fades + scales in once tire exits */}
      <div className={`${s.content} ${phase === 'done' || !visible ? s.contentVisible : ''}`}>
        {children}
      </div>

      {/* Overlay + tire */}
      {visible && (
        <div className={`${s.overlay} ${phase === 'done' ? s.overlayFade : ''}`}>
          <div className={`${s.tire} ${s[`tire_${phase}`]}`}>
            <TireSVG />
          </div>
        </div>
      )}
    </>
  )
}

/* F1-style slick tire SVG — tread blocks, hub, spoke details */
function TireSVG() {
  return (
    <svg viewBox="0 0 80 80" fill="none" xmlns="http://www.w3.org/2000/svg" className={s.tireSvg}>
      {/* Outer tyre sidewall */}
      <circle cx="40" cy="40" r="38" stroke="#2a2a2a" strokeWidth="2" fill="#111" />

      {/* Tread blocks — 12 blocks around the circumference */}
      {Array.from({ length: 12 }).map((_, i) => {
        const angle = (i / 12) * 360
        const rad = (angle * Math.PI) / 180
        const x = 40 + 33 * Math.cos(rad)
        const y = 40 + 33 * Math.sin(rad)
        return (
          <rect
            key={i}
            x={x - 3} y={y - 2}
            width={6} height={4}
            rx={0.5}
            fill="#1a1a1a"
            stroke="#333"
            strokeWidth="0.5"
            transform={`rotate(${angle} ${x} ${y})`}
          />
        )
      })}

      {/* Tyre face */}
      <circle cx="40" cy="40" r="28" fill="#0d0d0d" stroke="#222" strokeWidth="1" />

      {/* Spokes — 5 spokes */}
      {Array.from({ length: 5 }).map((_, i) => {
        const angle = (i / 5) * 360
        const rad = (angle * Math.PI) / 180
        const x2 = 40 + 22 * Math.cos(rad)
        const y2 = 40 + 22 * Math.sin(rad)
        return (
          <line
            key={i}
            x1="40" y1="40"
            x2={x2} y2={y2}
            stroke="#2a2a2a"
            strokeWidth="3"
            strokeLinecap="round"
          />
        )
      })}

      {/* Hub cap */}
      <circle cx="40" cy="40" r="8" fill="#161616" stroke="#333" strokeWidth="1" />
      <circle cx="40" cy="40" r="4" fill="#b8ff35" />
      <circle cx="40" cy="40" r="2" fill="#0d0d0d" />

      {/* Green accent ring — McLaren-style rim detail */}
      <circle cx="40" cy="40" r="24" stroke="rgba(184,255,53,0.15)" strokeWidth="1" fill="none" />
    </svg>
  )
}