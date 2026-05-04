import { mountRemoteApp } from './remoteAppMount'
import './localWebEntry.css'

if (typeof document !== 'undefined' && document.getElementById('root')) {
  mountRemoteApp()
}
