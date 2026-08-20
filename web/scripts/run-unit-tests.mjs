import { spawnSync } from 'node:child_process'
import fs from 'node:fs/promises'
import path from 'node:path'

const SRC_DIR = path.resolve('src')
const TEST_FILE_PATTERN = /\.(?:test|spec)\.(?:[cm]?[jt]sx?)$/

async function findTestFiles(dir) {
  const files = []
  const entries = await fs.readdir(dir, { withFileTypes: true })
  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      files.push(...(await findTestFiles(fullPath)))
    } else if (TEST_FILE_PATTERN.test(entry.name)) {
      files.push(fullPath)
    }
  }
  return files
}

function runTests(label, command, args, env = process.env) {
  console.log(`\n=== ${label} ===`)
  const result = spawnSync(command, args, {
    env,
    stdio: 'inherit',
  })
  if (result.error) throw result.error
  if (result.status !== 0) process.exit(result.status ?? 1)
}

const bunTests = []
const vitestTests = []
for (const file of (await findTestFiles(SRC_DIR)).sort()) {
  const source = await fs.readFile(file, 'utf8')
  const relative = path.relative(process.cwd(), file)
  const usesLegacyRunner =
    source.includes('bun:test') || source.includes('node:test')
  const usesVitest = /from\s+['"]vitest['"]/.test(source)

  if (usesLegacyRunner) {
    bunTests.push(relative)
  } else if (usesVitest) {
    vitestTests.push(relative)
  } else {
    throw new Error(`Cannot determine test runner for ${relative}`)
  }
}

runTests('Bun and Node unit tests', 'bun', ['test', ...bunTests])

const nodeEnv = { ...process.env }
if (Number.parseInt(process.versions.node, 10) >= 25) {
  nodeEnv.NODE_OPTIONS = [
    process.env.NODE_OPTIONS,
    '--no-experimental-webstorage',
  ]
    .filter(Boolean)
    .join(' ')
}
runTests(
  'Vitest unit tests',
  process.execPath,
  [path.resolve('node_modules/vitest/vitest.mjs'), 'run', ...vitestTests],
  nodeEnv
)
