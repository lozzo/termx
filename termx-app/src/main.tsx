import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import { TermxApp } from './TermxApp'

const root = document.getElementById('root')
if (!root) throw new Error('root element not found')

createRoot(root).render(
  <StrictMode>
    <TermxApp />
  </StrictMode>,
)
