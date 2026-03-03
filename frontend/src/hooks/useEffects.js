import { useEffect } from 'react'

export function useCursor() {
  useEffect(() => {
    const dot  = document.querySelector('.cursor-dot')
    const ring = document.querySelector('.cursor-ring')
    if (!dot || !ring) return

    let mx = -100, my = -100
    let rx = -100, ry = -100
    let raf

    const move = e => { mx = e.clientX; my = e.clientY }

    const tick = () => {
      // dot snaps
      dot.style.left = mx + 'px'
      dot.style.top  = my + 'px'
      // ring lerps
      rx += (mx - rx) * 0.14
      ry += (my - ry) * 0.14
      ring.style.left = rx + 'px'
      ring.style.top  = ry + 'px'
      raf = requestAnimationFrame(tick)
    }

    const over = e => {
      if (e.target.closest('button,a,[data-mag],input,textarea')) {
        dot.classList.add('hovered')
        ring.classList.add('hovered')
      }
    }
    const out = () => {
      dot.classList.remove('hovered')
      ring.classList.remove('hovered')
    }
    const down = () => dot.classList.add('clicking')
    const up   = () => dot.classList.remove('clicking')

    window.addEventListener('mousemove', move)
    window.addEventListener('mouseover', over)
    window.addEventListener('mouseout', out)
    window.addEventListener('mousedown', down)
    window.addEventListener('mouseup', up)
    raf = requestAnimationFrame(tick)

    return () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseover', over)
      window.removeEventListener('mouseout', out)
      window.removeEventListener('mousedown', down)
      window.removeEventListener('mouseup', up)
      cancelAnimationFrame(raf)
    }
  }, [])
}

export function useMagnetic() {
  useEffect(() => {
    const els = document.querySelectorAll('[data-mag]')
    const handlers = []

    els.forEach(el => {
      const strength = parseFloat(el.dataset.mag) || 0.35

      const onMove = e => {
        const r  = el.getBoundingClientRect()
        const cx = r.left + r.width / 2
        const cy = r.top  + r.height / 2
        const dx = e.clientX - cx
        const dy = e.clientY - cy
        el.style.transform = `translate(${dx * strength}px, ${dy * strength}px)`
        el.style.transition = 'transform 0.15s ease'
      }
      const onLeave = () => {
        el.style.transform = 'translate(0,0)'
        el.style.transition = 'transform 0.4s cubic-bezier(0.22,1,0.36,1)'
      }

      el.addEventListener('mousemove', onMove)
      el.addEventListener('mouseleave', onLeave)
      handlers.push({ el, onMove, onLeave })
    })

    return () => {
      handlers.forEach(({ el, onMove, onLeave }) => {
        el.removeEventListener('mousemove', onMove)
        el.removeEventListener('mouseleave', onLeave)
      })
    }
  })
}

export function useReveal() {
  useEffect(() => {
    const els = document.querySelectorAll('.reveal')
    const io  = new IntersectionObserver(
      entries => entries.forEach(e => {
        if (e.isIntersecting) {
          e.target.classList.add('visible')
          io.unobserve(e.target)
        }
      }),
      { threshold: 0.1 }
    )
    els.forEach(el => io.observe(el))
    return () => io.disconnect()
  })
}