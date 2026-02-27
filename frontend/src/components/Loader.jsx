import { useState, useEffect } from 'react'
import styles from './Loader.module.css'


const BOOT_SEQUENCE = [
  { text: 'INITIALIZING NODE IDENTITY...', delay: 300 },
  { text: 'LOADING CCN ROUTING TABLE...', delay: 280 },
  { text: 'BINDING UDP MULTICAST SOCKET...', delay: 350 },
  { text: 'SCANNING LAN INTERFACES...', delay: 400 },
  { text: 'CHECKING PEER REGISTRY...', delay: 260 },
  { text: 'MOUNTING CONTENT STORE...', delay: 300 },
  { text: 'VERIFYING NODE KEYPAIR...', delay: 320 },
  { text: 'ESTABLISHING WEBSOCKET LINK...', delay: 280 },
  { text: 'ALL SYSTEMS NOMINAL.', delay: 200 },
]

export default function Loader() {

  const [logLines, setLogLines] = useState([])     
  const [progress, setProgress] = useState(0)       
  const [done, setDone] = useState(false)           
  const [visible, setVisible] = useState(true)      

  useEffect(() => {
    let stepIndex = 0
    let totalDelay = 0

    BOOT_SEQUENCE.forEach((step, i) => {
      totalDelay += step.delay

      setTimeout(() => {
  
        setLogLines(prev => [...prev, step.text])
        setProgress(Math.round(((i + 1) / BOOT_SEQUENCE.length) * 100))
        if (i === BOOT_SEQUENCE.length - 1) {
          setTimeout(() => {
            setDone(true)                    
            setTimeout(() => setVisible(false), 600)  
          }, 500)
        }
      }, totalDelay)
    })
  }, []) 

 
  if (!visible) return null

  return (
    <div className={`${styles.overlay} ${done ? styles.fadeOut : ''}`}>
      {/* Background grid */}
      <div className={styles.grid} />

      <div className={styles.panel}>
        {/* Logo */}
        <div className={styles.logo}>
          <span className={styles.logoAccent}>LAN</span> SUITE
        </div>
        <div className={styles.version}>v0.1.0 — PHASE 1 BOOTSTRAP</div>

        {/* Progress bar */}
        <div className={styles.barWrap}>
          <div
            className={styles.bar}
            style={{ width: `${progress}%` }}
          />
          {/* The bar head glow follows the progress */}
          <div
            className={styles.barGlow}
            style={{ left: `${progress}%` }}
          />
        </div>
        <div className={styles.barLabel}>{progress}%</div>

        {/* Log terminal */}
        <div className={styles.terminal}>
          {logLines.map((line, i) => (
            
            <div key={i} className={styles.logLine}>
              <span className={styles.prompt}>$</span>
              <span className={styles.logText}>{line}</span>
              {/* Blinking cursor only on the last line */}
              {i === logLines.length - 1 && (
                <span className={styles.cursor} />
              )}
            </div>
          ))}
        </div>

        {/* Node ID */}
        <div className={styles.nodeId}>
          NODE // {generateNodeId()}
        </div>
      </div>
    </div>
  )
}
function generateNodeId() {
  return Array.from(crypto.getRandomValues(new Uint8Array(6)))
    .map(b => b.toString(16).padStart(2, '0').toUpperCase())
    .join(':')
}