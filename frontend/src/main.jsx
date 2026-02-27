import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/global.css' 
import App from './App.jsx'

/*
  CONCEPT: React's rendering model
  ─────────────────────────────────
  The browser's real DOM is slow to update.
  React keeps a "Virtual DOM" (a JS object tree) in memory.
  When state changes, React diffs the old vs new virtual DOM,
  then makes the *minimum* real DOM changes needed.
  This is why React is fast.

  createRoot() is React 18's API. It enables "concurrent mode"
  which lets React pause/resume rendering work (better for
  animations and large lists).
*/

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <App />
  </StrictMode>,
)