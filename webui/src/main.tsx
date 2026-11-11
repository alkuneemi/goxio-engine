import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { paintTheme } from './theme'
import './index.css'
import App from './App.tsx'

paintTheme()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
