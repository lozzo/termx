declare module '*?raw' {
  const content: string
  export default content
}

interface ImportMetaEnv {
  readonly VITE_CONTROL_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
  glob<T = unknown>(
    pattern: string,
    options?: {
      eager?: boolean
      import?: string
      query?: string
    },
  ): Record<string, T>
}
