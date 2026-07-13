import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import HomePage from './HomePage'
import LoginPage from './LoginPage'
import AccountPage from './AccountPage'
import './globals.css'
import './controller.css'

document.documentElement.dataset.wxTheme = localStorage.getItem('termx-wx-theme') === 'neutral-dark' ? 'neutral-dark' : 'light-gray'
const path=window.location.pathname.replace(/\/$/,'')||'/'
const Page=path==='/login'?LoginPage:path==='/account'?AccountPage:HomePage
createRoot(document.getElementById('root')!).render(<StrictMode><Page/></StrictMode>)
