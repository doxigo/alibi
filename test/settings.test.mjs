import assert from 'node:assert/strict'
globalThis.chrome = { runtime: { getManifest: () => ({ permissions: [] }) } }
const { checkedCover, hasCoordinates } = await import('../js/sites.js')
const { checkedImport, checkedSettingsCover, exportSettings, settingsChange, settingsWriter } = await import('../js/settings.js')
const tokyo = { configuration: 'custom', timezone: 'Asia/Tokyo', locale: 'ja-JP', lat: 0, lon: 0 }
assert.ok(hasCoordinates(tokyo))
assert.deepEqual(checkedCover(tokyo), { timezone: 'Asia/Tokyo', locale: 'ja-JP', lat: 0, lon: 0 })
for (const pair of [[null, 10], [91, 0], [0, 181], [Infinity, 0], [true, 0], [' ', 0]]) {
  assert.throws(() => checkedCover({ ...tokyo, lat: pair[0], lon: pair[1] }))
}
const backup = exportSettings({ ...tokyo, sites: { 'a.test': tokyo }, profiles: { Work: tokyo } })
assert.equal(checkedImport(backup).sites['a.test'].lat, 0)
assert.throws(() => checkedImport({ ...backup, version: 2 }))
assert.throws(() => checkedSettingsCover({ ...tokyo, injected: true }))
assert.throws(() => checkedSettingsCover({ ...tokyo, useDebuggerApi: 'false' }))
assert.throws(() => checkedImport({ ...backup, sites: { 'https://evil.test': { ...tokyo, lat: 1000 } } }))
const expired = { ...tokyo, expiresAt: 10 }
assert.deepEqual(settingsChange({ sites: { 'a.test': expired, 'b.test': tokyo } }, { command: 'expire' }, 11).sites, { 'b.test': tokyo })
assert.equal(settingsChange({ sites: { 'a.test': expired } }, { command: 'cover', site: 'a.test', changes: { expiresAt: null } }).sites['a.test'].expiresAt, undefined)
let state = { ...tokyo, sites: { 'a.test': { ...tokyo, configuration: 'ipAddress' }, 'b.test': tokyo } }
const write = settingsWriter({
  get: async () => structuredClone(state),
  set: async (change) => { await new Promise((resolve) => setTimeout(resolve, 5)); Object.assign(state, change) },
})
await Promise.all([
  write({ command: 'ip', cover: { timezone: 'Europe/Oslo', locale: 'nb-NO', lat: 59, lon: 10 } }),
  write({ command: 'cover', site: 'b.test', changes: { locale: 'en-US' } }),
])
assert.equal(state.sites['a.test'].timezone, 'Europe/Oslo')
assert.equal(state.sites['b.test'].locale, 'en-US')
await assert.rejects(write({ command: 'cover', changes: { lat: 200 } }))
await write({ command: 'profile', name: '__proto__', cover: tokyo })
assert.ok(Object.hasOwn(state.profiles, '__proto__'))
assert.equal({}.timezone, undefined)
await write({ command: 'cover', site: '__proto__', changes: tokyo })
assert.ok(Object.hasOwn(state.sites, '__proto__'))
assert.equal(Object.getPrototypeOf(state.sites), Object.prototype)
console.log('ok settings validation, expiry, import and concurrent writes')

const { importPlan } = await import('../js/settings.js')
const oldSite = { ...tokyo, expiresAt: 9100000000000 }
const prior = { ...tokyo, useDebuggerApi: true, sites: { 'a.test': oldSite }, profiles: { Work: tokyo } }
const imported = { version: 1, cover: tokyo, sites: { 'a.test': tokyo, 'new.test': tokyo }, profiles: { Work: { ...tokyo, locale: 'en-GB' } } }
const preview = importPlan(prior, imported)
const action = { command: 'import', data: imported, preview }
const firstImport = settingsChange({}, { ...action, preview: importPlan({}, imported) })
assert.equal(settingsChange(firstImport, { command: 'undoImport' }).configuration, 'browserDefault')
assert.equal(exportSettings({ timezone: 'Asia/Tokyo' }).cover.configuration, 'custom')
assert.throws(() => settingsChange({ ...prior, sites: {} }, action), /Preview/)
const merged = { ...prior, ...settingsChange(prior, action) }
assert.equal(merged.useDebuggerApi, false)
const later = { ...tokyo, locale: 'de-DE' }
merged.profiles.Work = later
merged.sites['other.test'] = later
const undone = { ...merged, ...settingsChange(merged, { command: 'undoImport' }) }
assert.equal(undone.useDebuggerApi, true)
assert.deepEqual(undone.sites['a.test'], oldSite)
assert.equal(undone.sites['new.test'], undefined)
assert.deepEqual(undone.sites['other.test'], later)
assert.deepEqual(undone.profiles.Work, later)
assert.equal(undone.importUndo, null)
const recovered = settingsChange(prior, { command: 'recoverSite', site: 'sub.a.test' }).sites
assert.equal(recovered['sub.a.test'].expiresAt, oldSite.expiresAt)
assert.equal(recovered['sub.a.test'].useDebuggerApi, false)
assert.deepEqual(recovered['a.test'], oldSite)
