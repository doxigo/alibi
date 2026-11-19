import assert from 'node:assert/strict'

const event = () => {
  const listeners = new Set()
  return { addListener: (fn) => listeners.add(fn), removeListener: (fn) => listeners.delete(fn),
    fire: (...args) => Promise.all([...listeners].map((fn) => fn(...args))) }
}
const changed = event()
const local = { configuration: 'custom', timezone: 'Asia/Tokyo', locale: 'ja-JP', useDebuggerApi: true,
  sites: { 'stealth.test': { configuration: 'custom', timezone: 'Europe/Oslo', locale: 'nb-NO', useDebuggerApi: false } } }
const session = {}
const area = (data, name) => ({
  get: async (keys) => Object.fromEntries((typeof keys === 'string' ? [keys] : keys).filter((key) => Object.hasOwn(data, key)).map((key) => [key, structuredClone(data[key])])),
  set: async (values) => {
    const changes = Object.fromEntries(Object.entries(values).map(([key, value]) => [key, { oldValue: data[key], newValue: value }]))
    Object.assign(data, structuredClone(values))
    await changed.fire(changes, name)
  },
  remove: async (key) => { delete data[key] },
})
const calls = [], injections = []
const scripts = new Map([['stealth', { id: 'stealth', js: [{ code: 'older registration' }] }]])
let refuse = false, headerWrites = 0
const tabs = [{ id: 1, url: 'https://deep.test/' }, { id: 2, url: 'https://stealth.test/' }]
globalThis.chrome = {
  extension: { isAllowedIncognitoAccess: async () => false },
  runtime: { id: 'alibi', getURL: (path) => `chrome-extension://alibi/${path}`,
    getManifest: () => ({ permissions: ['userScripts'] }), onMessage: event(), onStartup: event(), onInstalled: event() },
  storage: { local: area(local, 'local'), session: area(session, 'session'), onChanged: changed },
  userScripts: {
    getScripts: async ({ ids } = {}) => [...scripts.values()].filter(({ id }) => !ids || ids.includes(id)),
    register: async (items) => { calls.push('register'); for (const s of items) scripts.set(s.id, s) },
    update: async (items) => { calls.push('update'); for (const s of items) scripts.set(s.id, s) },
    unregister: async ({ ids }) => { calls.push('unregister'); for (const id of ids) scripts.delete(id) },
  },
  declarativeNetRequest: { getDynamicRules: async () => [], updateDynamicRules: async () => { headerWrites++ } },
  privacy: { network: { webRTCIPHandlingPolicy: { get: async () => ({ value: 'default', levelOfControl: 'controllable_by_this_extension' }), onChange: event(), set: async () => {}, clear: async () => {} } } },
  alarms: { clear: async () => {}, create: async () => {}, get: async () => null, onAlarm: event() },
  tabs: { query: async () => tabs, get: async (id) => tabs.find((tab) => tab.id === id), onCreated: event(), onRemoved: event() },
  webNavigation: { onBeforeNavigate: event(), onCommitted: event(), getFrame: async ({ tabId }) => ({ url: tabs.find((tab) => tab.id === tabId).url }) },
  debugger: { attach: async () => {}, detach: async ({ tabId }) => calls.push(`detach:${tabId}`),
    sendCommand: async ({ tabId }, method, params) => {
      calls.push({ tabId, method, params })
      if (refuse && method === 'Emulation.setTimezoneOverride' && params.timezoneId) throw new Error('Refused')
    }, onEvent: event(), onDetach: event() },
  scripting: { executeScript: async (details) => { injections.push(details); return [{ result: { state: 'applied', early: false } }] } },
}
await import('../js/background.js')
const flush = () => new Promise((resolve) => setTimeout(resolve, 20))
await flush()
assert.equal(calls.filter((call) => call === 'update').length, 1)
assert.ok(!calls.includes('unregister'), 'startup must retain the registered script')
const pageScript = scripts.get('stealth').js[0].code
await chrome.webNavigation.onBeforeNavigate.fire({ tabId: 1, frameId: 0, url: tabs[0].url })
assert.equal(session['protection-1'].state, 'applied')
await chrome.webNavigation.onCommitted.fire({ tabId: 2, frameId: 0, url: tabs[1].url, documentId: 'document-2' })
assert.equal(injections.length, 1, 'a registered user script must not disable fallback checks')
assert.deepEqual(injections[0].target.documentIds, ['document-2'])
assert.match(session['protection-2'].detail, /Early scripts/)
await chrome.storage.local.set({ sites: { ...local.sites, 'stealth.test': { ...local.sites['stealth.test'], locale: 'en-US' } } })
await flush()
assert.ok(!calls.some((call) => typeof call === 'string' && call.startsWith('detach:')))
assert.equal(session['protection-2'].state, 'reload-required')
const deepCommands = calls.filter((call) => call.tabId === 1).length
const beforeProfile = headerWrites
await chrome.storage.local.set({ profiles: { Work: { timezone: 'Asia/Tokyo' } } })
await flush()
assert.equal(calls.filter((call) => call.tabId === 1).length, deepCommands)
assert.equal(headerWrites, beforeProfile)
assert.ok(!calls.includes('unregister'))
refuse = true
await chrome.storage.local.set({ timezone: 'Europe/Berlin' })
await flush()
assert.equal(headerWrites, beforeProfile)
assert.equal(session['protection-1'].state, 'failed')
refuse = false
await chrome.webNavigation.onBeforeNavigate.fire({ tabId: 1, frameId: 0, url: tabs[0].url })
assert.equal(session['protection-1'].state, 'applied')
await chrome.debugger.onDetach.fire({ tabId: 1 }, 'canceled_by_user')
assert.equal(session['protection-1'].state, 'failed')
await chrome.storage.local.set({ sites: { ...local.sites, 'deep.test': { configuration: 'custom', timezone: 'Asia/Tokyo', useDebuggerApi: true, expiresAt: Date.now() + 50 } } })
await new Promise((resolve) => setTimeout(resolve, 60))
await chrome.alarms.onAlarm.fire({ name: 'expireCovers' })
await flush()
assert.ok(!Object.hasOwn(local.sites, 'deep.test'))
assert.equal(calls.filter((call) => call.tabId === 1 && call.method === 'Emulation.setTimezoneOverride' && call.params.timezoneId).at(-1).params.timezoneId, 'Europe/Berlin')
await chrome.storage.local.set({ sites: { ...local.sites, 'deep.test': { configuration: 'custom', timezone: 'Asia/Tokyo', useDebuggerApi: true, sealWebRtc: true } } })
await flush()
assert.equal(session['protection-1'].state, 'reload-required', 'Deep cannot replace an existing page-level WebRTC seal live')
await chrome.storage.local.set({ sites: {}, timezone: null, locale: null })
await flush()
await chrome.webNavigation.onBeforeNavigate.fire({ tabId: 1, frameId: 0, url: tabs[0].url })
assert.equal(session['protection-1'].state, 'off')
await chrome.tabs.onRemoved.fire(1)
assert.ok(!session['protection-1'])
const ask = (message) => new Promise((resolve) => chrome.runtime.onMessage.fire(message, { id: 'alibi', url: 'chrome-extension://alibi/html/manage.html' }, resolve))
globalThis.fetch = async () => ({ ok: true, json: async () => ({ country_code: 'JP', timezone: 'Asia/Tokyo', latitude: '0', longitude: '0' }) })
assert.equal((await ask({ lookupIp: true })).cover.lat, 0)
const lastSuccess = local.ipStatus.lastSuccess
globalThis.fetch = async () => { throw new Error('offline') }
assert.equal((await ask({ lookupIp: true })).ok, false)
assert.equal(local.ipStatus.lastSuccess, lastSuccess)
assert.equal(local.ipStatus.error, 'offline')
console.log('ok background: script updates, fallback, scoped changes, failure/retry, detach and expiry')

const native = { onMessage: event(), onDisconnect: event(), postMessage() {}, disconnect() {} }
chrome.runtime.connectNative = () => native
await chrome.storage.local.set({ configuration: 'ipAddress' })
await flush()
const place = { timezone: 'Europe/Oslo', locale: 'nb-NO', lat: 59, lon: 10 }
const checked = Date.now()
await native.onMessage.fire({ ...place, lookupStatus: { checked, lastSuccess: checked } })
assert.equal(local.timezone, place.timezone)
await flush()
await native.onMessage.fire({ ...place, lookupStatus: { checked: Date.now(), lastSuccess: checked, failed: true } })
assert.equal(local.ipStatus.lastSuccess, checked)
assert.match(local.ipStatus.error, /Companion/)
globalThis.fetch = async () => ({ ok: true, json: async () => ({ country_code: 'JP', timezone: 'Asia/Tokyo', latitude: 35, longitude: 139 }) })
await chrome.alarms.onAlarm.fire({ name: 'matchIp' })
await flush()
assert.equal(local.timezone, 'Asia/Tokyo', 'failed companion allows browser fallback')
assert.equal(local.ipStatus.source, 'GeoJS')
await native.onMessage.fire({ ...place, lookupStatus: { checked: checked - 1000, lastSuccess: checked - 1000 } })
assert.equal(local.timezone, 'Asia/Tokyo', 'late observations cannot replace a newer location')
globalThis.fetch = async () => { throw new Error('next browser retry') }
await chrome.alarms.onAlarm.fire({ name: 'matchIp' })
await flush()
assert.equal(local.ipStatus.error, 'next browser retry', 'a stale native reply cannot suppress the next browser check')

const siteRules = Object.fromEntries(Array.from({ length: 1000 }, (_, i) => [`private-${i}.test`, { ...place, useDebuggerApi: false }]))
await chrome.storage.local.set({ sites: siteRules })
await flush()
assert.ok(scripts.get('alibi-selector').js[0].code.includes('private-999.test'))
assert.equal(scripts.get('stealth').js[0].code, pageScript, '1,000 site rules add no code to the page script')
