import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { WebControlRemoteApp } from './WebControlRemoteApp'
import './localWebEntry.css'

const root = document.getElementById('root')
if (!root) {
  throw new Error('web control remote root element is required')
}

createRoot(root).render(
  <StrictMode>
    <WebControlRemoteApp defaultControlUrl="http://114.66.58.243:12306" />
  </StrictMode>,
)
