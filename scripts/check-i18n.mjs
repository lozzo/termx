import { readFileSync } from 'node:fs'

const localeGroups = [
  {
    name: 'shared App UI',
    files: {
      en: 'clients/ui/src/locales/en.json',
      'zh-CN': 'clients/ui/src/locales/zh-CN.json',
    },
  },
  {
    name: 'Web Controller',
    files: {
      en: 'private/cloud/web-controller/web/src/locales/en.json',
      'zh-CN': 'private/cloud/web-controller/web/src/locales/zh-CN.json',
    },
  },
]

for (const group of localeGroups) {
  const entries = Object.entries(group.files).map(([language, path]) => [
    language,
    flatten(JSON.parse(readFileSync(path, 'utf8'))),
  ])
  const [referenceLanguage, referenceKeys] = entries[0]
  for (const [language, keys] of entries.slice(1)) {
    const missing = [...referenceKeys].filter((key) => !keys.has(key))
    const extra = [...keys].filter((key) => !referenceKeys.has(key))
    if (missing.length || extra.length) {
      throw new Error(`${group.name} locale ${language} differs from ${referenceLanguage}\nmissing: ${missing.join(', ') || '-'}\nextra: ${extra.join(', ') || '-'}`)
    }
  }
  console.log(`${group.name}: ${referenceKeys.size} locale keys match`)
}

function flatten(value, prefix = '', output = new Set()) {
  if (Array.isArray(value)) {
    value.forEach((item, index) => flatten(item, `${prefix}[${index}]`, output))
    return output
  }
  if (value && typeof value === 'object') {
    for (const [key, child] of Object.entries(value)) {
      flatten(child, prefix ? `${prefix}.${key}` : key, output)
    }
    return output
  }
  output.add(prefix)
  return output
}
