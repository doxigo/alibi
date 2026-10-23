// Builds the unpacked extension into build/: yarn build
// With --store, also the Chrome Web Store zip, alibi-store.zip: yarn build:store
// Plain Node rather than rm, cp and zip, so it runs the same on Windows, macOS and Linux.
import { cpSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { sep } from 'node:path'
import { crc32, deflateRawSync } from 'node:zlib'

const root = new URL('..', import.meta.url)
const build = new URL('build/', root)

rmSync(build, { recursive: true, force: true })
for (const entry of ['html', 'css', 'img', 'fonts', 'js', '_locales', 'manifest.json']) {
  cpSync(new URL(entry, root), new URL(entry, build), { recursive: true })
}
rmSync(new URL('img/src', build), { recursive: true, force: true })

// Little-endian fields, as [bytes, value] pairs.
const fields = (...pairs) =>
  Buffer.concat(pairs.map(([bytes, value]) => {
    const b = Buffer.alloc(bytes)
    b.writeUIntLE(value, 0, bytes)
    return b
  }))

// A plain deflate zip of everything in dir, with paths relative to it and forward slashes on every OS,
// so manifest.json sits at the root. Every entry is dated 1980-01-01, so the same build makes the same zip.
// Header layouts: APPNOTE.TXT 4.3.7 (local file), 4.3.12 (central directory), 4.3.16 (end of it).
const zip = (dir) => {
  const names = readdirSync(dir, { recursive: true })
    .map((name) => name.split(sep).join('/'))
    .filter((name) => statSync(new URL(name, dir)).isFile())
    .sort()
  const files = []
  const entries = []
  let offset = 0
  for (const name of names) {
    const data = readFileSync(new URL(name, dir))
    const packed = deflateRawSync(data)
    const path = Buffer.from(name)
    // Shared by both headers: version 2.0, UTF-8 name, deflate, time, date, CRC, sizes, name and extra lengths.
    const shared = fields([2, 20], [2, 0x800], [2, 8], [2, 0], [2, 0x21], [4, crc32(data)], [4, packed.length], [4, data.length], [2, path.length], [2, 0])
    files.push(fields([4, 0x04034b50]), shared, path, packed)
    // Then no comment, disk 0, no attributes, and where the local header starts.
    entries.push(fields([4, 0x02014b50], [2, 20]), shared, fields([2, 0], [2, 0], [2, 0], [4, 0], [4, offset]), path)
    offset += 30 + path.length + packed.length
  }
  const directory = Buffer.concat(entries)
  const end = fields([4, 0x06054b50], [2, 0], [2, 0], [2, names.length], [2, names.length], [4, directory.length], [4, offset], [2, 0])
  return Buffer.concat([...files, directory, end])
}

if (process.argv.includes('--store')) {
  // The store build leaves out the permissions the store won't take, and the key that pins the ID.
  const file = new URL('manifest.json', build)
  const manifest = JSON.parse(readFileSync(file, 'utf8'))
  manifest.permissions = manifest.permissions.filter((p) => !['userScripts', 'nativeMessaging'].includes(p))
  delete manifest.key
  writeFileSync(file, JSON.stringify(manifest, null, 2))
  writeFileSync(new URL('alibi-store.zip', root), zip(build))
}
