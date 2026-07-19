import { registerPlugin } from '@capacitor/core'

export interface PickedFile {
  uri: string
  name: string
  size: number
  mimeType: string
}

interface NativeFilePickerPlugin {
  pickFiles(options?: { multiple?: boolean }): Promise<{ files: PickedFile[] }>
  saveFile(options: { name: string; mimeType?: string; dataBase64: string }): Promise<{
    uri: string
    path: string
    bytes: number
    sha256: string
  }>
}

const NativeFilePicker = registerPlugin<NativeFilePickerPlugin>('NativeFilePicker')

export default NativeFilePicker
