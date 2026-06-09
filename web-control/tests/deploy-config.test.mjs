import { readFile } from 'node:fs/promises'
import test from 'node:test'
import assert from 'node:assert/strict'

test('web-control deployment env example documents required control-plane settings', async () => {
  const env = await readFile(new URL('../.env.example', import.meta.url), 'utf8')

  for (const name of [
    'JWT_SECRET',
    'HUB_SECRET',
    'APP_URL',
    'SQLITE_PATH',
    'GITHUB_CLIENT_ID',
    'GITHUB_CLIENT_SECRET',
  ]) {
    assert.match(env, new RegExp(`(^|\\n)#?\\s*${name}=`), `${name} missing from .env.example`)
  }

  assert.match(env, /HUB_SECRET.*TERMX_HUB_CONTROL_SECRET|TERMX_HUB_CONTROL_SECRET.*HUB_SECRET/s)
  assert.match(env, /GitHub OAuth/i)
})

test('web-control deployment guide covers minimal startup without migrations', async () => {
  const deploy = await readFile(new URL('../DEPLOY.md', import.meta.url), 'utf8')

  for (const text of [
    'cp .env.example .env',
    'npm run dev',
    '/api/health',
    '/api/auth/register',
    'ensureSqliteSchema',
  ]) {
    assert.match(deploy, new RegExp(escapeRegExp(text)), `${text} missing from DEPLOY.md`)
  }

  assert.match(deploy, /no database migration command is required/i)
})

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}
