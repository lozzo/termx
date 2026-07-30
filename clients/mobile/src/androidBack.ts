import { useEffect } from 'react'
import { App as CapApp } from '@capacitor/app'
import { Capacitor } from '@capacitor/core'
import { dispatchNativeBack } from '@anytty/ui'

export function handleAndroidBackButton(): void {
  if (dispatchNativeBack()) return
  if (!Capacitor.isNativePlatform() || Capacitor.getPlatform() !== 'android') return
  if (!Capacitor.isPluginAvailable('App')) return

  try {
    void CapApp.exitApp().catch(() => undefined)
  } catch {}
}

export function useAndroidBackButton(): void {
  useEffect(() => {
    if (!Capacitor.isNativePlatform() || Capacitor.getPlatform() !== 'android') return undefined
    if (!Capacitor.isPluginAvailable('App')) return undefined

    let subscription: ReturnType<typeof CapApp.addListener>
    try {
      subscription = CapApp.addListener('backButton', handleAndroidBackButton)
    } catch {
      return undefined
    }
    void subscription.catch(() => undefined)
    return () => {
      void subscription.then((handle) => handle.remove()).catch(() => undefined)
    }
  }, [])
}
