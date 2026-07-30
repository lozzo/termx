import { defineConfig } from '@playwright/test'
import base from './playwright.config'

const projectNames = new Set(['desktop-chromium', 'mobile-320-chromium'])

export default defineConfig({
  ...base,
  grep: /@axe/,
  grepInvert: [],
  projects: base.projects?.filter((project) => projectNames.has(project.name ?? '')).map((project) => ({
    ...project,
    use: {
      ...project.use,
      colorScheme: project.name === 'mobile-320-chromium' ? 'dark' : 'light',
    },
  })),
})
