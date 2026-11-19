import { applyDebugger, forgetChild, forgetDebugger, startChild } from './debugger.js'
import {
  checkedCover, coverFor, coverKey, followsIp, holdsPlace, ipCover, isActive, languagesFor, pageCover, sameZone, siteKey, systemCover,
  IP_MAX_AGE, SEALED_POLICY, userScriptsAvailable,
} from './sites.js'
import stealth from './stealth.js'
import { SETTINGS_KEYS, settingsWriter } from './settings.js'

const KEYS = SETTINGS_KEYS.filter((key) => key !== 'profiles')
const rtcPolicy = chrome.privacy.network.webRTCIPHandlingPolicy
const effectiveSeal = async () => {
  if ((await rtcPolicy.get({})).value !== SEALED_POLICY) return false
  return !(await chrome.extension.isAllowedIncognitoAccess()) || (await rtcPolicy.get({ incognito: true })).value === SEALED_POLICY
}
let browserSeal = effectiveSeal().catch(() => false)
const writeSettings = settingsWriter(chrome.storage.local)
const report = (tabId, status) => chrome.storage.session.set({ [`protection-${tabId}`]: { ...status, updated: Date.now() } })
const failed = (tabId, error, mode = 'Deep') => report(tabId, { state: 'failed', mode, detail: String(error.message ?? error).slice(0, 240) })
// Kept as a promise so a warm worker answers instantly and a cold one still waits for storage.
let settings = chrome.storage.local.get(KEYS)

// Only the selected cover crosses from Chrome's private user-script world into the page.
// The two scripts can start in either order. Both sides finish synchronously at document_start.
function receiveCover(apply) {
  window.addEventListener('alibi-cover', (event) => {
    event.preventDefault()
    const cover = JSON.parse(event.detail)
    if (cover) apply({ ...cover, documentStart: true })
  }, { once: true })
  window.dispatchEvent(new Event('alibi-ready'))
}
function deliverCover(resolve, fallback) {
  const origins = location.ancestorOrigins
  let host = ''
  try { host = new URL(origins?.length ? origins[origins.length - 1] : location.href).hostname } catch {}
  let cover = fallback
  for (let h = host; h; h = h.slice(h.indexOf('.') + 1 || h.length)) {
    const match = resolve(h)
    if (match !== undefined) { cover = match; break }
  }
  const send = () => window.dispatchEvent(new CustomEvent('alibi-cover', { detail: JSON.stringify(cover), cancelable: true }))
  if (send()) window.addEventListener('alibi-ready', send, { once: true })
}
const registerUserScript = async () => {
  if (!userScriptsAvailable()) return false
  const [current, sealed] = await Promise.all([settings, browserSeal])
  const fallback = pageCover(current, '', sealed)
  const groups = new Map()
  let active = !!fallback
  for (const [host, rule] of Object.entries(current.sites ?? {})) {
    if (rule.expiresAt && rule.expiresAt <= Date.now()) continue
    const cover = pageCover(current, host, sealed)
    active ||= !!cover
    const deadline = rule.expiresAt ? `if(Date.now()>=${JSON.stringify(rule.expiresAt)})return;` : ''
    const result = `${deadline}return ${JSON.stringify(cover)};`
    if (!groups.has(result)) groups.set(result, [])
    groups.get(result).push(`case ${JSON.stringify(host)}:`)
  }
  const scripts = []
  const add = (id, world, code) => scripts.push({ id, world, matches: ['<all_urls>'], allFrames: true, runAt: 'document_start', js: [{ code }] })
  if (active && groups.size) {
    const cases = Array.from(groups, ([result, labels]) => labels.join('') + result).join('')
    add('alibi-selector', 'USER_SCRIPT', `(${deliverCover})(h=>{switch(h){${cases}}},${JSON.stringify(fallback)})`)
    add('stealth', 'MAIN', `(${receiveCover})(${stealth})`)
  } else if (active) add('stealth', 'MAIN', `(${stealth})(${JSON.stringify({ ...fallback, documentStart: true })})`)
  const existing = await chrome.userScripts.getScripts({ ids: ['stealth', 'alibi-selector'] })
  const create = scripts.filter((script) => !existing.some((old) => old.id === script.id))
  const update = scripts.filter((script) => existing.some((old) => old.id === script.id && old.js?.[0]?.code !== script.js[0].code))
  const remove = existing.filter((old) => !scripts.some((script) => script.id === old.id)).map(({ id }) => id)
  if (create.length) await chrome.userScripts.register(create)
  if (update.length) await chrome.userScripts.update(update)
  if (remove.length) await chrome.userScripts.unregister({ ids: remove })
  return active
}
// Serialize registration updates without removing the working script.
let userScriptReady = registerUserScript().catch(() => false)
const syncUserScript = () =>
  (userScriptReady = userScriptReady.then(registerUserScript).catch((e) => (console.error(e), false)))

// The Accept-Language header, in both modes: Deep's CDP override can land after a page's own
// request has already left, so the header is set here, in the network layer, where timing can't miss.
// A site rule wins over All sites, and a more specific site over its parent domain.
// Chrome 145 added topDomains. Older browsers can only match the initiating frame.
const ALL_TYPES = ['main_frame', 'sub_frame', 'stylesheet', 'script', 'image', 'font', 'object', 'xmlhttprequest',
  'ping', 'csp_report', 'media', 'websocket', 'webtransport', 'webbundle', 'other']
const headerAction = (locale) =>
  locale
    ? { type: 'modifyHeaders', requestHeaders: [{ header: 'accept-language', operation: 'set',
        value: languagesFor(locale).map((l, i) => (i ? `${l};q=0.9` : l)).join(',') }] }
    : { type: 'allow' } // a site on its real location keeps the browser's own header
const syncHeaderRules = async () => {
  const { locale, sites = {} } = await settings
  const scope = chrome.declarativeNetRequest.RuleConditionKeys?.TOP_DOMAINS ? 'topDomains' : 'initiatorDomains'
  const rules = []
  if (locale) rules.push({ priority: 1, action: headerAction(locale), condition: { resourceTypes: ALL_TYPES } })
  for (const [site, cover] of Object.entries(sites)) {
    if (cover.expiresAt && cover.expiresAt <= Date.now()) continue
    const priority = 1 + site.split('.').length
    const action = headerAction(cover.locale)
    rules.push(
      { priority, action, condition: { requestDomains: [site], resourceTypes: ['main_frame'] } },
      { priority, action, condition: { [scope]: [site], resourceTypes: ALL_TYPES.slice(1) } },
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

// WebRTC asks a STUN server, over UDP, which address it sees. UDP can go around a proxy, or a VPN that
// doesn't take every route, and the answer is the real IP, which the page can read. Sealed, Chrome sends
// WebRTC through the proxy only, or with no proxy over TCP alone, which the VPN carries, and asks no STUN
// server. It's one browser-wide setting, so it holds for every site, in either mode. While it's off, a site
// can still seal WebRTC on its own: its pages then keep WebRTC to TCP relays themselves (stealth.js).
const syncWebRtc = async () => {
  const { sealWebRtc } = await chrome.storage.local.get('sealWebRtc')
  const { levelOfControl, value } = await rtcPolicy.get({})
  if (!sealWebRtc) await rtcPolicy.clear({})
  else if (levelOfControl === 'controllable_by_this_extension' || levelOfControl === 'controlled_by_this_extension' && value !== SEALED_POLICY) {
    await rtcPolicy.set({ value: SEALED_POLICY })
  }
}
rtcPolicy.onChange.addListener(async ({ levelOfControl }) => {
  const previous = browserSeal
  browserSeal = effectiveSeal().catch(() => false)
  const [before, after, current] = await Promise.all([previous, browserSeal, settings])
  if (current.sealWebRtc && levelOfControl === 'controllable_by_this_extension') syncWebRtc().catch(console.error)
  if (before === after) return
  syncUserScript()
  for (const tab of await chrome.tabs.query({})) {
    const cover = coverFor(siteKey(tab.url ?? '') ?? '', current)
    if (cover.site && cover.sealWebRtc) await report(tab.id, { state: 'reload-required', detail: 'WebRTC policy changed. Reload to update the site relay protection.' })
  }
})
syncWebRtc().catch(console.error)

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
let lastCompanionSuccess = 0
let heardHello = false // the host on `port` said hello
let takesCovers = false // and takes the All sites cover
let saidFollowing = null // what `port` was last told about following the IP
let posted = null // the cover last sent over `port`, as JSON
let probing = null // a timer while a port opened only to see whether a companion answers
const lookupIp = async (apply = false) => {
  const checked = Date.now()
  try {
    const cover = await ipCover()
    await writeSettings({ command: 'ip', cover: apply ? cover : null, status: { lastSuccess: checked, checked, source: 'GeoJS' } })
    return cover
  } catch (error) {
    await writeSettings({ command: 'ip', status: { checked, source: 'GeoJS', error: error.message } })
    throw error
  }
}
const companionLocation = (message) => {
  const now = Date.now()
  const status = message.lookupStatus ?? { checked: now, lastSuccess: now }
  if (!Number.isSafeInteger(status.checked) || status.checked <= 0 || !Number.isSafeInteger(status.lastSuccess) ||
      status.lastSuccess < 0 || status.lastSuccess > status.checked) return
  const checked = Math.min(now, status.checked)
  const lastSuccess = Math.min(checked, status.lastSuccess)
  const cover = status.failed ? null : checkedCover(message)
  return writeSettings({ command: 'ip', cover, status: { checked, lastSuccess, source: 'companion',
    ...(status.failed ? { error: 'Companion location lookup failed.' } : {}) } }).then(({ ipStatus }) => {
    if (ipStatus.source === 'companion' && ipStatus.checked === checked) lastCompanionSuccess = status.failed ? 0 : lastSuccess
  })
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
    post({ following, lookupStatus: true })
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
  lastCompanionSuccess = 0
  heardHello = takesCovers = false
  saidFollowing = posted = null
  systemHeld(null)
}
const connectWhereabouts = () => {
  if (port || !chrome.runtime.connectNative) return
  port = chrome.runtime.connectNative(WHEREABOUTS)
  port.onMessage.addListener((message) => {
    if (message?.hello) return hello(message)
    if (message && 'system' in message) return systemHeld(message.system)
    return companionLocation(message)?.catch(console.error)
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
  if (!(await syncIpAlarm()) || Date.now() - lastCompanionSuccess < IP_MAX_AGE) return
  await lookupIp(true).catch(() => {})
}
syncIpAlarm() // alarms can be lost on update or restart
chrome.alarms.onAlarm.addListener(({ name }) => {
  if (name === IP_ALARM) refreshIpCovers()
  if (name === COMPANION_ALARM) syncIpAlarm()
  if (name === 'expireCovers') writeSettings({ command: 'expire' }).then(scheduleExpiry).catch(console.error)
})
chrome.runtime.onStartup.addListener(() => {
  refreshIpCovers() // Chrome may have started on another network
  probeCompanion()
})

// The popup opening asks again too, with a place for all sites and no companion known to take it: its
// switch for following Alibi may have been turned on since, with no host running to say hello again.
const scheduleExpiry = async () => {
  const { sites = {} } = await settings
  const deadlines = Object.values(sites).map((rule) => rule.expiresAt).filter(Number.isFinite)
  await chrome.alarms.clear('expireCovers')
  if (deadlines.length) await chrome.alarms.create('expireCovers', { when: Math.max(Date.now() + 100, Math.min(...deadlines)) })
}
writeSettings({ command: 'expire' }).then(scheduleExpiry).catch(console.error)

chrome.runtime.onMessage.addListener((message, sender, respond) => {
  if (sender.id !== chrome.runtime.id || !sender.url?.startsWith(chrome.runtime.getURL('html/'))) return
  if (message?.lookupIp) {
    lookupIp().then((cover) => respond({ ok: true, cover }), (error) => respond({ ok: false, error: error.message }))
    return true
  }
  if (message?.settings) {
    if (!['cover', 'removeSite', 'profile', 'removeProfile', 'import', 'undoImport'].includes(message.settings.command)) {
      respond({ ok: false, error: 'Unknown settings request.' })
      return
    }
    writeSettings(message.settings).then((stored) => respond({ ok: true, stored }), (error) => respond({ ok: false, error: error.message }))
    return true
  }
  if (Number.isInteger(message?.recoverDeep)) {
    const tabId = message.recoverDeep
    const recover = async () => {
      const tab = await chrome.tabs.get(tabId)
      const site = siteKey(tab.url ?? '')
      const key = `protection-${tabId}`
      const status = (await chrome.storage.session.get(key))[key]
      if (!site || site !== message.site || status?.state !== 'failed' || status.mode !== 'Deep') throw new Error('The tab or its status changed. Check it again.')
      await writeSettings({ command: 'recoverSite', site })
      settings = chrome.storage.local.get(KEYS)
      await Promise.all([syncUserScript(), syncHeaders()])
      if (siteKey((await chrome.tabs.get(tabId)).url ?? '') === site) await chrome.tabs.reload(tabId)
    }
    recover().then(() => respond({ ok: true }), (error) => respond({ ok: false, error: error.message }))
    return true
  }
  if (Number.isInteger(message?.status)) {
    const tabId = message.status
    Promise.all([chrome.storage.session.get(`protection-${tabId}`), settings, chrome.tabs.get(tabId), chrome.storage.local.get('ipStatus')])
      .then(([saved, stored, tab, ip]) => respond({ ok: true, site: siteKey(tab.url ?? ''), status: saved[`protection-${tabId}`] ?? { state: 'configured', detail: 'Reload this tab to apply and check its cover.' }, cover: coverFor(siteKey(tab.url ?? '') ?? '', stored), ...ip }),
        (error) => respond({ ok: false, error: error.message }))
    return true
  }
  if (message?.checkCompanion) {
    chrome.storage.local.get(['configuration', 'timezone', 'companion'])
      .then((stored) => stored.companion !== true && holdsPlace(stored) && probeCompanion())
  }
})

chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== 'local') return
  if (changes.sealWebRtc) syncWebRtc().catch(console.error)
  if (!Object.keys(changes).some((key) => KEYS.includes(key))) return
  const before = settings
  settings = chrome.storage.local.get(KEYS)
  syncUserScript()
  if (changes.locale || changes.sites) syncHeaders()
  syncIpAlarm()
  scheduleExpiry()
  Promise.all([before, settings]).then(([old, current]) => reconcileTabs(old, current)).catch(console.error)
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
  const cover = coverFor(siteKey(url ?? '') ?? '', current)
  // A Stealth site: a tab that comes from a Deep one is detached. With no Deep site at all, none is attached.
  if (!cover.useDebuggerApi) return {}
  // As in Stealth: an IP cover on the Mac's own zone leaves the zone live, whichever name each gives it.
  return followsIp(cover) && sameZone(cover.timezone, Intl.DateTimeFormat().resolvedOptions().timeZone) ? { ...cover, timezone: null } : cover
}
const aimTab = async (tabId, url, replacing = false) => {
  try {
    const cover = await deepCover(url)
    const result = await applyDebugger(tabId, cover, replacing)
    if (result === 'unreachable') return result
    if (cover.useDebuggerApi) await report(tabId, { state: isActive(cover) ? 'applied' : 'off', mode: 'Deep', key: coverKey(cover),
      detail: isActive(cover) ? 'Browser overrides applied. Run diagnostics to check what the page observes.' : 'No location cover is configured for this site.' })
  } catch (error) {
    await failed(tabId, error)
  }
}
const reconcileTabs = async (old, current) => {
  for (const tab of await chrome.tabs.query({})) {
    const host = siteKey(tab.url ?? '')
    if (!host) continue
    const previous = coverFor(host, old, 0)
    const next = coverFor(host, current)
    if (coverKey(previous) === coverKey(next) && old.sealWebRtc === current.sealWebRtc) continue
    if (previous.useDebuggerApi && next.useDebuggerApi && !!previous.sealWebRtc === !!next.sealWebRtc && !!old.sealWebRtc === !!current.sealWebRtc) await aimTab(tab.id, tab.url)
    else await report(tab.id, { state: 'reload-required', detail: "Settings changed. Reload to replace this document's cover." })
  }
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
    const committed = committedBlank(tabId)
    await chrome.tabs.update(tabId, { url: 'about:blank' })
    await committed
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
  await report(tabId, { state: 'configured', detail: "Applying this page's cover." })
  if ((await aimTab(tabId, url, true)) === 'unreachable' && /^https?:/.test(url)) bounce(tabId, url, await deepCover(url))
})
// Again at commit, for anything that still slipped past. An unchanged cover is a no-op, so this
// never disturbs a tab that's already right.
chrome.webNavigation.onCommitted.addListener(({ tabId, frameId, url }) => frameId === 0 && !bouncing.has(tabId) && aimTab(tabId, url))
// Cross-site iframes and workers wait, paused, for the cover of the page they sit in.
chrome.debugger.onEvent.addListener((source, method, params) => {
  if (method === 'Target.detachedFromTarget') forgetChild(source.tabId, params.sessionId)
  if (method !== 'Target.attachedToTarget') return
  startChild(source, params, async () => (await deepCover((await chrome.tabs.get(source.tabId)).url)))
    .catch((error) => failed(source.tabId, error))
})
chrome.debugger.onDetach.addListener(({ tabId }, reason) => {
  forgetDebugger(tabId)
  failed(tabId, new Error(`Deep mode detached (${reason}). Reload to apply it again.`))
})
chrome.tabs.onRemoved.addListener((tabId) => {
  forgetDebugger(tabId)
  chrome.storage.session.remove(`protection-${tabId}`)
})

// Always check this document, even if a registration completed after it started loading.
chrome.webNavigation.onCommitted.addListener(async ({ tabId, frameId, url, documentId }) => {
  try {
    const current = await settings
    const topUrl = frameId === 0 ? url : (await chrome.webNavigation.getFrame({ tabId, frameId: 0 }).catch(() => null))?.url
    const host = siteKey(topUrl ?? url) ?? ''
    if (!siteKey(topUrl ?? url)) return
    const effective = coverFor(host, current)
    const cover = pageCover(current, host, await browserSeal)
    if (effective.useDebuggerApi && !cover) return
    const results = await chrome.scripting.executeScript({
      target: documentId ? { tabId, documentIds: [documentId] } : { tabId, frameIds: [frameId] },
      world: 'MAIN', injectImmediately: true, func: stealth, args: [cover ?? {}],
    })
    if (frameId === 0 && !effective.useDebuggerApi) {
      const result = results[0]?.result
      await report(tabId, { state: result?.state ?? 'off', mode: 'Stealth', key: coverKey(effective),
        detail: result?.state === 'reload-required' ? 'Reload to replace the previous cover.' : result?.early
          ? 'Applied at document start. Stealth has worker and frame limits.' : cover
          ? 'Applied after navigation. Early scripts may have read real values.' : 'No location cover is configured for this site.' })
    }
    if (cover && !(await userScriptReady) && userScriptsAvailable()) syncUserScript()
  } catch (error) {
    if (frameId === 0 && /^https?:/.test(url)) failed(tabId, error, 'Stealth')
  }
})
