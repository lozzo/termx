import type { ComponentType } from 'react'
import type { FileManagerProps } from './FileManager'

export type FileManagerComponent = ComponentType<FileManagerProps>

export async function loadFileManager(): Promise<FileManagerComponent> {
  const module = await import('./FileManager')
  return module.FileManager
}
