import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'TermX - Your terminals, reachable anywhere',
  description: 'Direct P2P when possible. Managed Relay when networks disagree. Terminal authorization stays end to end.',
}

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  )
}
