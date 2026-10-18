import { isActive, languagesFor } from './sites.js'

// Cross-site iframes and workers run in their own renderer, which the tab's own emulation never
// reaches. Auto-attaching hands each one to us; with `pause` it is held at birth until it has the
// tab's cover. Chrome can leave a cross-site iframe stuck loading forever, never announcing it, if
// auto-attach is switched on while that iframe is mid-navigation. So it is only switched on when
// nothing can be mid-load: just before the tab's page is replaced, or inside a paused child.
const autoAttach = (target, pause) =>
  send(target, 'Target.setAutoAttach', { autoAttach: true, waitForDebuggerOnStart: pause, flatten: true })

// The cover CDP currently holds for each tab, as { key, cover }. Overrides survive navigations, so
// an unchanged cover is never resent: resending means clear-then-set, a gap early scripts can see.
// Frames and workers take this cover too, since they belong to the tab's page.
const applied = new Map()
// Tabs whose auto-attach is on. It persists in Chrome, so it is switched on once, never re-sent.
const armed = new Set()

const send = (target, method, params) => chrome.debugger.sendCommand(target, method, params).catch(() => {})

// The browser's own user agent and client hints, re-sent unchanged: an override that sets only
// Accept-Language would otherwise blank navigator.userAgentData, a giveaway of its own.
const userAgent = (async () => {
  const data = navigator.userAgentData
  if (!data) return null
  const high = await data.getHighEntropyValues(['architecture', 'bitness', 'fullVersionList', 'model', 'platformVersion', 'wow64'])
  return {
    userAgent: navigator.userAgent,
    userAgentMetadata: {
      brands: data.brands, mobile: data.mobile, platform: data.platform, fullVersionList: high.fullVersionList,
      platformVersion: high.platformVersion, architecture: high.architecture, model: high.model, bitness: high.bitness, wow64: high.wow64,
    },
  }
})()

// Sent back to back, not awaited one by one: CDP runs them in order. Chrome refuses a second
// override while one is active, so each is cleared before it's set. Resolves once all have run.
const emulate = (target, { timezone, locale, lat, lon }) =>
  Promise.all([
    send(target, 'Emulation.setTimezoneOverride', { timezoneId: '' }),
    timezone && send(target, 'Emulation.setTimezoneOverride', { timezoneId: timezone }),
    send(target, 'Emulation.setLocaleOverride', {}),
    locale && send(target, 'Emulation.setLocaleOverride', { locale }),
    send(target, 'Emulation.clearGeolocationOverride'),
    (lat || lon) && send(target, 'Emulation.setGeolocationOverride', { latitude: lat, longitude: lon, accuracy: 1 }),
    // Sets navigator.language(s) (and the header, which background.js also sets for timing's sake).
    // Chrome adds the q-values itself.
    // A cover without a locale gets the browser's own list back.
    userAgent.then((ua) => ua && send(target, 'Emulation.setUserAgentOverride', {
      ...ua, acceptLanguage: (locale ? languagesFor(locale) : navigator.languages).join(','),
    })),
  ])

// Deep mode: point the tab's CDP emulation at this cover. A tab with nothing to spoof is
// detached so its banner goes away. `replacing` is true just before the tab's page navigates away.
// Resolves 'unreachable' while the tab shows a browser page (chrome://), which can't be debugged.
// ponytail: Chrome holds renderer commands until a cross-site navigation commits, so when the
// cover *changes* between two sites, a page that arrives instantly (measured at 0ms latency, not
// at 60ms) can run its first inline scripts with the previous cover. Iframes and workers are exact.
const applyDebugger = async (tabId, cover, replacing = false) => {
  if (!isActive(cover)) {
    applied.delete(tabId)
    armed.delete(tabId)
    await chrome.debugger.detach({ tabId }).catch(() => {}) // throws when we weren't attached
    return
  }
  // getTargets() also reports other debuggers and briefly misreports new tabs, so just try.
  let unreachable = false
  const fresh = await chrome.debugger.attach({ tabId }, '1.3').then(
    () => true,
    (error) => ((unreachable = error.message.includes('chrome://')), false), // otherwise: already attached
  )
  if (unreachable) return 'unreachable'
  if (fresh) {
    applied.delete(tabId)
    armed.delete(tabId)
  }
  // A late attach (e.g. after the new tab page) covers this page's own scripts only; its cross-site
  // frames get the cover from the next navigation on, once auto-attach is safe to switch on.
  if (replacing && !armed.has(tabId)) {
    armed.add(tabId)
    autoAttach({ tabId }, true)
  }
  const key = JSON.stringify([cover.timezone, cover.locale, cover.lat, cover.lon])
  if (applied.get(tabId)?.key === key) return
  applied.set(tabId, { key, cover })
  emulate({ tabId }, cover)
}

// A frame or worker attached under a Deep-mode tab: give it the cover and let it carry on
// auto-attaching its own children. One attached while already running (Deep attached late,
// e.g. after the new tab page) still gets the cover; one attached paused is always resumed,
// within a second even if Chrome never answers, because a child left paused hangs the page.
// `lookUpCover` is only asked when this worker hasn't applied a cover to the tab itself (it restarted).
const startChild = async (source, { sessionId, waitingForDebugger }, lookUpCover) => {
  const child = { tabId: source.tabId, sessionId }
  try {
    const cover = async () => applied.get(source.tabId)?.cover ?? lookUpCover()
    const ready = (async () => Promise.all([autoAttach(child, waitingForDebugger), emulate(child, await cover())]))()
    await Promise.race([ready, new Promise((resolve) => setTimeout(resolve, 1000))])
  } finally {
    if (waitingForDebugger) send(child, 'Runtime.runIfWaitingForDebugger')
  }
}

const detachDebugger = async () => {
  applied.clear()
  armed.clear()
  for (const target of await chrome.debugger.getTargets()) {
    if (target.attached && target.tabId) chrome.debugger.detach({ tabId: target.tabId }).catch(() => {})
  }
}

export { applyDebugger, detachDebugger, startChild }
