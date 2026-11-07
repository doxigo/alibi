import { hasCoordinates, isActive, languagesFor } from './sites.js'

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
const attached = new Set()
const children = new Map()
const pending = new Map()
const desired = new Map()
const childErrors = new Map()

const send = (target, method, params) => chrome.debugger.sendCommand(target, method, params).catch((error) => {
  if (/only available to trusted clients/.test(error.message)) {
    throw new Error('This browser blocks Deep mode overrides. Choose Stealth and reload; its documented worker and frame limits apply.')
  }
  throw error
})

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
const emulate = (target, { timezone, locale, lat, lon }, worker = false) =>
  Promise.allSettled([
    send(target, 'Emulation.setTimezoneOverride', { timezoneId: '' }),
    timezone && send(target, 'Emulation.setTimezoneOverride', { timezoneId: timezone }),
    send(target, 'Emulation.setLocaleOverride', {}),
    locale && send(target, 'Emulation.setLocaleOverride', { locale }),
    !worker && send(target, 'Emulation.clearGeolocationOverride'),
    !worker && hasCoordinates({ lat, lon }) && send(target, 'Emulation.setGeolocationOverride', { latitude: lat, longitude: lon, accuracy: 1 }),
    // Sets navigator.language(s) (and the header, which background.js also sets for timing's sake).
    // Chrome adds the q-values itself.
    // A cover without a locale gets the browser's own list back.
    userAgent.then((ua) => ua && send(target, 'Emulation.setUserAgentOverride', {
      ...ua, acceptLanguage: (locale ? languagesFor(locale) : navigator.languages).join(','),
    })),
  ]).then((results) => {
    const failure = results.find((result) => result.status === 'rejected')
    if (failure) throw failure.reason
  })

// Deep mode: point the tab's CDP emulation at this cover. A tab with nothing to spoof is
// detached so its banner goes away. `replacing` is true just before the tab's page navigates away.
// Resolves 'unreachable' while the tab shows a browser page (chrome://), which can't be debugged.
// ponytail: Chrome holds renderer commands until a cross-site navigation commits, so when the
// cover *changes* between two sites, a page that arrives instantly (measured at 0ms latency, not
// at 60ms) can run its first inline scripts with the previous cover. Iframes and workers are exact.
const apply = async (tabId, cover, replacing) => {
  childErrors.delete(tabId)
  if (!isActive(cover)) {
    if (attached.has(tabId)) await chrome.debugger.detach({ tabId })
    forgetDebugger(tabId)
    return
  }
  if (!attached.has(tabId)) {
    try {
      await chrome.debugger.attach({ tabId }, '1.3')
    } catch (error) {
      if (error.message.includes('chrome://')) return 'unreachable'
      // A surviving session after a worker restart must prove it belongs to us.
      await send({ tabId }, 'Runtime.getIsolateId')
    }
    attached.add(tabId)
  }
  if (replacing && !armed.has(tabId)) {
    await autoAttach({ tabId }, true)
    armed.add(tabId)
  }
  const key = JSON.stringify([cover.timezone, cover.locale, cover.lat, cover.lon])
  if (applied.get(tabId)?.key === key) return
  await emulate({ tabId }, cover)
  await Promise.all([...(children.get(tabId)?.values() ?? [])].map(({ target, worker }) => emulate(target, cover, worker)))
  if (childErrors.has(tabId)) throw childErrors.get(tabId)
  applied.set(tabId, { key, cover })
}

const applyDebugger = (tabId, cover, replacing = false) => {
  desired.set(tabId, cover)
  const job = (pending.get(tabId) ?? Promise.resolve()).catch(() => {}).then(() => apply(tabId, cover, replacing))
  pending.set(tabId, job)
  job.then(() => {
    if (pending.get(tabId) === job) pending.delete(tabId)
  }, () => {
    applied.delete(tabId)
    if (pending.get(tabId) === job) pending.delete(tabId)
  })
  return job
}

const startChild = async (source, { sessionId, waitingForDebugger, targetInfo }, lookUpCover) => {
  const child = { tabId: source.tabId, sessionId }
  const worker = /worker/.test(targetInfo?.type ?? '')
  if (!children.has(source.tabId)) children.set(source.tabId, new Map())
  children.get(source.tabId).set(sessionId, { target: child, worker })
  let timer
  try {
    const cover = desired.get(source.tabId) ?? applied.get(source.tabId)?.cover ?? await lookUpCover()
    await Promise.race([
      Promise.all([autoAttach(child, waitingForDebugger), emulate(child, cover, worker)]),
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('A frame or worker could not be covered in time. Reload the tab.')), 1000) }),
    ])
  } catch (error) {
    applied.delete(source.tabId)
    childErrors.set(source.tabId, error)
    throw error
  } finally {
    clearTimeout(timer)
    if (waitingForDebugger) await send(child, 'Runtime.runIfWaitingForDebugger')
  }
}

const forgetChild = (tabId, sessionId) => children.get(tabId)?.delete(sessionId)
const forgetDebugger = (tabId) => {
  applied.delete(tabId)
  armed.delete(tabId)
  attached.delete(tabId)
  children.delete(tabId)
  desired.delete(tabId)
  childErrors.delete(tabId)
}

export { applyDebugger, forgetChild, forgetDebugger, startChild }
