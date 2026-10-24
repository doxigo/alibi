// Builds Alibi Companion for Windows and Linux into companion/dist/: yarn build:companion
// Needs Go (https://go.dev/dl/). The version comes from manifest.json, so each companion matches the
// Alibi release it ships with. macOS has Whereabouts, so there's no macOS build.
import { execFileSync } from 'node:child_process'
import { mkdirSync, readFileSync, rmSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const root = new URL('..', import.meta.url)
const companion = new URL('companion/', root)
const { version } = JSON.parse(readFileSync(new URL('manifest.json', root), 'utf8'))
const dist = new URL('dist/', companion)

const targets = [
  ['windows', 'amd64', 'alibi-companion-windows-x64.exe'],
  ['windows', 'arm64', 'alibi-companion-windows-arm64.exe'],
  ['linux', 'amd64', 'alibi-companion-linux-x64'],
  ['linux', 'arm64', 'alibi-companion-linux-arm64'],
]
const go = (args, env = {}) => execFileSync('go', args, { cwd: companion, env: { ...process.env, ...env }, stdio: 'inherit' })

rmSync(dist, { recursive: true, force: true })
mkdirSync(dist, { recursive: true })
// The Windows programs' icon (Alibi's stamp), version information and manifest, which the linker
// builds in from these files. They're made fresh for each build, for its version.
const icons = [16, 32, 48, 128].map((size) => fileURLToPath(new URL(`img/icon-${size}.png`, root)))
const resources = ['amd64', 'arm64'].map((arch) => `rsrc_windows_${arch}.syso`)
try {
  for (const [i, arch] of ['amd64', 'arm64'].entries()) {
    go(['run', './cmd/winres', '-version', version, '-arch', arch, '-o', resources[i], ...icons])
  }
  for (const [os, arch, name] of targets) {
    // -trimpath and no cgo: the same source builds the same program on any machine.
    go(['build', '-trimpath', '-ldflags', `-s -w -X main.version=${version}`, '-o', fileURLToPath(new URL(name, dist)), '.'],
      { GOOS: os, GOARCH: arch, CGO_ENABLED: '0' })
    console.log(`companion/dist/${name}`)
  }
} finally {
  for (const file of resources) rmSync(new URL(file, companion), { force: true })
}
