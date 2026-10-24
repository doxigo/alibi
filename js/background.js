import { applyDebugger, startChild } from './debugger.js'
import {
  checkedCover, coverFor, followIp, followsIp, holdsPlace, ipCover, isActive, languagesFor, sameZone, siteKey, systemCover,
  userScriptsAvailable,
} from './sites.js'
import stealth from './stealth.js'

const KEYS = ['useDebuggerApi', 'configuration', 'timezone', 'locale', 'lat', 'lon', 'sites']
// Kept as a promise so a warm worker answers instantly and a cold one still waits for storage.
let settings = chrome.storage.local.get(KEYS)

const stealthCover = (settings, url) => {
  if (settings.useDebuggerApi) return null
  const { configuration, timezone, locale, lat, lon } = coverFor(siteKey(url) ?? '', settings)
  return isActive({ timezone, locale, lat, lon }) ? { configuration, timezone, locale, lat, lon } : null
}

// Bake every cover into one user script so Chrome runs it at document_start, before any page script.
// The script picks its cover from the top-level site, so iframes tell the same story as their page.
// Resolves true when a stealth user script is registered.
const registerUserScript = async () => {
  if (!userScriptsAvailable()) return false
  const { useDebuggerApi, sites = {}, ...everywhere } = await settings
  await chrome.userScripts.unregister()
  if (useDebuggerApi || !(isActive(everywhere) || Object.values(sites).some(isActive))) return false
  const topHost = `(() => {
    const origins = location.ancestorOrigins
    try { return new URL(origins?.length ? origins[origins.length - 1] : location.href).hostname } catch { return '' }
  })()`
  await chrome.userScripts.register([{
    id: 'stealth',
    matches: ['<all_urls>'],
    allFrames: true,
    runAt: 'document_start',
    world: 'MAIN',
    js: [{ code: `(${stealth})((${coverFor})(${topHost}, ${JSON.stringify({ ...everywhere, sites })}))` }],
  }])
  return true
}
// Chained so overlapping syncs never race unregister() against register().
let userScriptReady = registerUserScript().catch(() => false)
const syncUserScript = () =>
  (userScriptReady = userScriptReady.then(registerUserScript).catch((e) => (console.error(e), false)))

// The Accept-Language header, in both modes: Deep's CDP override can land after a page's own
// request has already left, so the header is set here, in the network layer, where timing can't miss.
// A site rule wins over All sites, and a more specific site over its parent domain.
// ponytail: a cross-site iframe's own requests get the cover of the frame's site, not the page's;
// matching by top-level page needs per-tab session rules.
const ALL_TYPES = ['main_frame', 'sub_frame', 'stylesheet', 'script', 'image', 'font', 'object', 'xmlhttprequest',
  'ping', 'csp_report', 'media', 'websocket', 'webtransport', 'webbundle', 'other']
const headerAction = (locale) =>
  locale
    ? { type: 'modifyHeaders', requestHeaders: [{ header: 'accept-language', operation: 'set',
        value: languagesFor(locale).map((l, i) => (i ? `${l};q=0.9` : l)).join(',') }] }
    : { type: 'allow' } // a site on its real location keeps the browser's own header
const syncHeaderRules = async () => {
  const { locale, sites = {} } = await settings
  const rules = []
  if (locale) rules.push({ priority: 1, action: headerAction(locale), condition: { resourceTypes: ALL_TYPES } })
  for (const [site, cover] of Object.entries(sites)) {
    const priority = 1 + site.split('.').length
    const action = headerAction(cover.locale)
    rules.push(
      { priority, action, condition: { requestDomains: [site], resourceTypes: ['main_frame'] } },
      { priority, action, condition: { initiatorDomains: [site], resourceTypes: ALL_TYPES.slice(1) } },
    )
  }
  const old = await chrome.declarativeNetRequest.getDynamicRules()
  await chrome.declarativeNetRequest.updateDynamicRules({
    removeRuleIds: old.map((r) => r.id),
    addRules: rules.map((rule, i) => ({ id: i + 1, ...rule })),
  })
}
let headerRulesReady = syncHeaderRules().catch(console.error)
const syncHeaders = () => (headerRulesReady = headerRulesReady.then(syncHeaderRules).catch(console.error))

// "Match my IP address" follows the connection. While any cover uses it, Alibi keeps a native port
// open to Whereabouts, the companion Mac app, or on Windows and Linux to Alibi Companion (companion/),
// which answers to the same name. Either one reports each new VPN exit the moment it sees one.
// The open port also keeps this worker alive. Without them (not installed, not running, or a
// store build without the permission) GeoJS is asked again every minute instead, so a new exit
// moves the cover within a minute. Pages pick it up when they next load, except a time zone that
// Whereabouts moves on the Mac, which reaches open pages live (see stealth.js).
//
// Alibi says whether it's following the IP ({ following }) the moment the port opens, so a companion
// that looks the connection up itself asks GeoJS nothing while no cover needs it. A companion that can
// keep the system's own clock on Alibi's location right now (Whereabouts, Alibi Companion on Windows,
// with Follow Alibi's location on) says so in its hello ({ hello, accepts: ['cover'] }), and says hello
// again if that changes. Alibi then sends it the All sites cover whenever it changes ({ cover }), and
// keeps the port open while that cover is a place, which the companion holds only for as long as the
// port stays open. It answers with the zone it holds the system on ({ system }), which the popup shows;
// pages keep their own cover either way. One that says no hello, such as an older Whereabouts, gets no
// cover, as before.
const IP_ALARM = 'matchIp'
const COMPANION_ALARM = 'companion' // retries the port while a place waits for a companion that isn't up
const WHEREABOUTS = 'com.doxigo.whereabouts'
let port = null
let whereaboutsLive = false // Whereabouts has reported over `port`, so GeoJS stands down
let heardHello = false // the host on `port` said hello
let takesCovers = false // and takes the All sites cover
let saidFollowing = null // what `port` was last told about following the IP
let posted = null // the cover last sent over `port`, as JSON
let probing = null // a timer while a port opened only to see whether a companion answers
const applyIp = async (ip) => {
  // Read after the lookup, so a change made in the popup meanwhile isn't overwritten.
  const changes = followIp(await chrome.storage.local.get(['configuration', 'timezone', 'locale', 'lat', 'lon', 'sites']), ip)
  if (Object.keys(changes).length) await chrome.storage.local.set(changes)
}
const post = (message) => {
  try {
    port?.postMessage(message)
  } catch {} // the port closed meanwhile
}
// Tells the host whether a cover follows the IP, and a companion that takes it the All sites cover,
// whenever either changes.
const tellCompanion = async () => {
  if (!port) return
  const stored = await chrome.storage.local.get(['configuration', 'timezone', 'locale', 'sites'])
  const following = followsIp(stored)
  if (following !== saidFollowing) {
    saidFollowing = following
    post({ following })
  }
  if (!takesCovers) return
  const cover = systemCover(stored)
  const json = JSON.stringify(cover)
  if (json === posted) return
  posted = json
  post({ cover })
}
const hello = async ({ accepts }) => {
  heardHello = true
  const takes = Array.isArray(accepts) && accepts.includes('cover')
  if (takes !== takesCovers) posted = null // starting to take covers: it gets the current one
  takesCovers = takes
  if ((await chrome.storage.local.get('companion')).companion !== takesCovers) await chrome.storage.local.set({ companion: takesCovers })
  syncIpAlarm() // a place can now be held, or a port opened only to ask can close
}
// The zone the companion holds the system on, or null, for the popup to show.
const systemHeld = async (zone) => {
  const systemZone = typeof zone === 'string' && zone ? zone : null
  if (((await chrome.storage.local.get('systemZone')).systemZone ?? null) !== systemZone) await chrome.storage.local.set({ systemZone })
}
const forgetPort = () => {
  port = null
  whereaboutsLive = heardHello = takesCovers = false
  saidFollowing = posted = null
  systemHeld(null)
}
const connectWhereabouts = () => {
  if (port || !chrome.runtime.connectNative) return
  port = chrome.runtime.connectNative(WHEREABOUTS)
  port.onMessage.addListener((message) => {
    if (message?.hello) return hello(message)
    if (message && 'system' in message) return systemHeld(message.system)
    const ip = checkedCover(message) // a malformed message throws and leaves the cover alone
    whereaboutsLive = true
    applyIp(ip)
  })
  port.onDisconnect.addListener(() => {
    // Not installed, or the app quit: the alarms take over and retry.
    const missing = /not found/i.test(chrome.runtime.lastError?.message ?? '')
    forgetPort()
    if (missing) chrome.storage.local.remove('companion')
  })
  tellCompanion()
}
const closePort = () => {
  port?.disconnect() // fires no onDisconnect on this side
  forgetPort()
}
// Opens the port for a moment to see whether a companion answers, and whether it takes the cover:
// when the browser starts, when Alibi is installed or updated, and when a place is chosen while no
// companion is known to take it (it may have started to: Follow Alibi's location switched on). The host
// that's started asks GeoJS nothing, since no cover follows the IP then.
const probeCompanion = () => {
  if (port || probing || !chrome.runtime.connectNative) return
  connectWhereabouts()
  probing = setTimeout(async () => {
    probing = null
    if (port && !heardHello && (await chrome.storage.local.get('companion')).companion === undefined) {
      await chrome.storage.local.set({ companion: false })
    }
    syncIpAlarm()
  }, 3000)
}
const syncIpAlarm = async () => {
  const stored = await chrome.storage.local.get(['configuration', 'timezone', 'sites', 'companion'])
  const following = followsIp(stored)
  // A companion holds a place only while its port is open.
  const holding = stored.companion === true && holdsPlace(stored)
  if (following || holding) connectWhereabouts()
  else if (port && !probing) closePort()
  tellCompanion()
  if (!following) await chrome.alarms.clear(IP_ALARM)
  else if (!(await chrome.alarms.get(IP_ALARM))) await chrome.alarms.create(IP_ALARM, { periodInMinutes: 1 })
  if (following || !holding) await chrome.alarms.clear(COMPANION_ALARM)
  else if (!(await chrome.alarms.get(COMPANION_ALARM))) await chrome.alarms.create(COMPANION_ALARM, { periodInMinutes: 1 })
  return following
}
const refreshIpCovers = async () => {
  if (!(await syncIpAlarm()) || whereaboutsLive) return
  const ip = await ipCover().catch(() => null) // offline, or GeoJS down: keep the last place
  if (ip && !whereaboutsLive) await applyIp(ip) // Whereabouts may have answered meanwhile
}
syncIpAlarm() // alarms can be lost on update or restart
chrome.alarms.onAlarm.addListener(({ name }) => {
  if (name === IP_ALARM) refreshIpCovers()
  if (name === COMPANION_ALARM) syncIpAlarm()
})
chrome.runtime.onStartup.addListener(() => {
  refreshIpCovers() // Chrome may have started on another network
  probeCompanion()
})

// What only says how the companion is doing changes no cover, so nothing is synced again for it.
const BOOKKEEPING = ['companion', 'systemZone']
chrome.storage.onChanged.addListener((changes) => {
  if (Object.keys(changes).every((key) => BOOKKEEPING.includes(key))) return
  settings = chrome.storage.local.get(KEYS)
  syncUserScript()
  syncHeaders()
  syncIpAlarm()
  if (changes.configuration || changes.timezone) {
    chrome.storage.local.get(['configuration', 'timezone', 'companion'])
      .then((stored) => stored.companion !== true && holdsPlace(stored) && probeCompanion())
  }
})

chrome.runtime.onInstalled.addListener((details) => {
  probeCompanion()
  if (details.reason === 'install') {
    chrome.tabs.create({
      active: true,
      url: 'html/info.html',
    })
  }
})

// Deep mode: CDP emulation per tab, re-aimed whenever the tab heads to another site.
// Accurate, but Chrome shows the "started debugging" banner while attached.
const deepCover = async (url) => {
  const current = await settings
  if (!current.useDebuggerApi) return null
  const cover = coverFor(siteKey(url ?? '') ?? '', current)
  // As in Stealth: an IP cover on the Mac's own zone leaves the zone live, whichever name each gives it.
  return followsIp(cover) && sameZone(cover.timezone, Intl.DateTimeFormat().resolvedOptions().timeZone) ? { ...cover, timezone: null } : cover
}
const aimTab = async (tabId, url, replacing) => {
  const cover = await deepCover(url)
  if (cover) applyDebugger(tabId, cover, replacing)
}
// The new tab page and other browser pages can't be debugged, so leaving one, Deep could only attach
// once the site was already loading: its first scripts and cross-site frames would miss the cover.
// Instead the tab is parked on about:blank, attached and armed there, then sent on with
// location.replace, which also takes the blank page back out of history. Everything that leaves
// these pages is a plain GET (typed address, tile, search), so re-issuing it loses nothing.
const bouncing = new Set()
const committedBlank = (tabId) =>
  new Promise((resolve) => {
    const done = () => (chrome.webNavigation.onCommitted.removeListener(listener), clearTimeout(timer), resolve())
    const listener = (d) => d.tabId === tabId && d.frameId === 0 && d.url === 'about:blank' && done()
    const timer = setTimeout(done, 2000)
    chrome.webNavigation.onCommitted.addListener(listener)
  })
const bounce = async (tabId, url, cover) => {
  bouncing.add(tabId)
  try {
    await chrome.tabs.update(tabId, { url: 'about:blank' })
    await committedBlank(tabId)
    await applyDebugger(tabId, cover, true)
    await chrome.debugger.sendCommand({ tabId }, 'Runtime.evaluate', { expression: `location.replace(${JSON.stringify(url)})` })
  } catch {
    chrome.tabs.update(tabId, { url }).catch(() => {}) // never strand the tab on the blank page
  } finally {
    bouncing.delete(tabId)
  }
}

// Earliest chance: a new tab, while it still shows the blank page its first navigation leaves.
chrome.tabs.onCreated.addListener((tab) => aimTab(tab.id, tab.pendingUrl ?? tab.url))
chrome.webNavigation.onBeforeNavigate.addListener(async ({ tabId, frameId, url }) => {
  if (frameId !== 0 || bouncing.has(tabId)) return
  const cover = await deepCover(url)
  if (cover && (await applyDebugger(tabId, cover, true)) === 'unreachable' && /^https?:/.test(url)) bounce(tabId, url, cover)
})
// Again at commit, for anything that still slipped past. An unchanged cover is a no-op, so this
// never disturbs a tab that's already right.
chrome.webNavigation.onCommitted.addListener(({ tabId, frameId, url }) => frameId === 0 && !bouncing.has(tabId) && aimTab(tabId, url))
// Cross-site iframes and workers wait, paused, for the cover of the page they sit in.
chrome.debugger.onEvent.addListener((source, method, params) => {
  if (method !== 'Target.attachedToTarget') return
  startChild(source, params, async () => (await deepCover((await chrome.tabs.get(source.tabId)).url)) ?? {})
})

// Stealth mode (default): patch each new document's JS globals. No debugger, no banner.
// With user scripts allowed, the registered script already ran at document_start. Otherwise inject
// on commit, which can lose the race against inline <head> scripts when the body arrives instantly.
chrome.webNavigation.onCommitted.addListener(async ({ tabId, frameId, url }) => {
  const current = await settings
  if (current.useDebuggerApi) return
  // Frames take the cover of the page they sit in.
  const topUrl = frameId === 0 ? url : (await chrome.webNavigation.getFrame({ tabId, frameId: 0 }).catch(() => null))?.url
  const cover = stealthCover(current, topUrl ?? url)
  if (!cover) return
  if (userScriptsAvailable()) {
    if (await userScriptReady) return
    syncUserScript() // toggle was switched on since the last sync; covers the next page load
  }
  chrome.scripting
    .executeScript({
      target: { tabId, frameIds: [frameId] },
      world: 'MAIN',
      injectImmediately: true,
      func: stealth,
      args: [cover],
    })
    .catch(() => {}) // chrome://, Web Store and other protected pages refuse injection
})
