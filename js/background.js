import { applyDebugger, startChild } from './debugger.js'
import { coverFor, isActive, siteKey, userScriptsAvailable } from './sites.js'
import stealth from './stealth.js'

const KEYS = ['useDebuggerApi', 'timezone', 'locale', 'lat', 'lon', 'sites']
// Kept as a promise so a warm worker answers instantly and a cold one still waits for storage.
let settings = chrome.storage.local.get(KEYS)

const stealthCover = (settings, url) => {
  if (settings.useDebuggerApi) return null
  const { timezone, locale, lat, lon } = coverFor(siteKey(url) ?? '', settings)
  return isActive({ timezone, locale, lat, lon }) ? { timezone, locale, lat, lon } : null
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

chrome.storage.onChanged.addListener(() => {
  settings = chrome.storage.local.get(KEYS)
  syncUserScript()
})

chrome.runtime.onInstalled.addListener((details) => {
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
  return current.useDebuggerApi ? coverFor(siteKey(url ?? '') ?? '', current) : null
}
const aimTab = async (tabId, url, replacing) => {
  const cover = await deepCover(url)
  if (cover) applyDebugger(tabId, cover, replacing)
}
// Earliest chance: a new tab, while it still shows the blank page its first navigation leaves.
chrome.tabs.onCreated.addListener((tab) => aimTab(tab.id, tab.pendingUrl ?? tab.url))
chrome.webNavigation.onBeforeNavigate.addListener(({ tabId, frameId, url }) => frameId === 0 && aimTab(tabId, url, true))
// Again at commit, for tabs that started on a page the debugger can't attach to (the new tab page).
// An unchanged cover is a no-op, so this never disturbs a tab that's already right.
chrome.webNavigation.onCommitted.addListener(({ tabId, frameId, url }) => frameId === 0 && aimTab(tabId, url))
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
