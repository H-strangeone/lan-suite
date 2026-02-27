import { useState, useEffect } from 'react'
import Landing from './pages/Landing.jsx'
import Loader from './components/Loader.jsx'

/*
  CONCEPT: React Component
  ─────────────────────────
  A component is a FUNCTION that returns JSX.
  JSX looks like HTML but it's actually JavaScript.
  <div className="foo"> compiles to React.createElement('div', {className:'foo'})

  Rules:
  1. Component names MUST start with uppercase (Landing, not landing)
     Lowercase = HTML element. Uppercase = React component.
  2. Must return a single root element (or <> </> fragment)
  3. Props flow DOWN (parent → child), events flow UP (child → parent via callbacks)

  CONCEPT: useState
  ──────────────────
  useState(initialValue) returns [currentValue, setterFunction]
  When you call the setter, React re-renders the component.
  State is LOCAL to the component unless you lift it up or use a store.

  CONCEPT: useEffect
  ───────────────────
  Runs side effects AFTER the component renders.
  The dependency array [] means "run once after first render" (like componentDidMount).
  If you pass [someVar], it runs every time someVar changes.
  If you pass nothing, it runs after EVERY render (usually a bug).
*/

export default function App() {
  // isLoading: true = show loader, false = show content
  const [isLoading, setIsLoading] = useState(true)

  // page: which "page" to show — later this will be a real router
  const [page, setPage] = useState('landing')

  useEffect(() => {
    // Simulate boot sequence — in real app this will be:
    // 1. Check if JWT token exists in memory
    // 2. Ping the Go backend health endpoint
    // 3. Discover LAN peers via WebSocket
    const timer = setTimeout(() => setIsLoading(false), 2800)

    // IMPORTANT: Return a cleanup function.
    // If the component unmounts before the timer fires,
    // this cancels the timer (prevents memory leaks).
    return () => clearTimeout(timer)
  }, []) // [] = run once on mount

  // While loading, show the Loader component
  if (isLoading) return <Loader />

  // Route to the right page
  // Later: replace this with React Router <Routes> / <Route>
  return (
    <div className="app">
      {page === 'landing' && <Landing onEnter={() => setPage('dashboard')} />}
      {page === 'dashboard' && (
        <div style={{ padding: '2rem', color: 'var(--acid)' }}>
          {/* Placeholder — Dashboard component comes in Block 3 */}
          Dashboard coming in Block 3 — signaling server must be built first.
          <button
            onClick={() => setPage('landing')}
            style={{ display:'block', marginTop:'1rem', color:'var(--muted)' }}
          >
            ← back to landing
          </button>
        </div>
      )}
    </div>
  )
}