import { mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { delimiter, dirname, join, relative, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(webRoot, '..', '..')
const protoRoot = join(repoRoot, 'proto')
const outputRoot = join(webRoot, 'src', 'generated')
const sources = readdirSync(join(protoRoot, 'cloud', 'v1'), { withFileTypes: true })
  .filter((entry) => entry.isFile() && entry.name.endsWith('.proto'))
  .map((entry) => relative(repoRoot, join(protoRoot, 'cloud', 'v1', entry.name)))
  .sort()

mkdirSync(outputRoot, { recursive: true })
const result = spawnSync('protoc', [
  `--es_out=${relative(repoRoot, outputRoot)}`,
  '--es_opt=target=ts,import_extension=none',
  '-I', relative(repoRoot, protoRoot),
  ...sources,
], {
  cwd: repoRoot,
  env: { ...process.env, PATH: `${join(repoRoot, 'node_modules', '.bin')}${delimiter}${process.env.PATH ?? ''}` },
  stdio: 'inherit',
})
if (result.error) throw result.error
if (result.status !== 0) process.exit(result.status ?? 1)

function normalize(directory) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) normalize(path)
    else if (entry.name.endsWith('_pb.ts')) writeFileSync(path, `${readFileSync(path, 'utf8').trimEnd()}\n`)
  }
}
normalize(outputRoot)
