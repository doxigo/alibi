// Per-site covers live in storage as
// `sites: { 'example.com': { configuration, timezone, locale, lat, lon, useDebuggerApi, sealWebRtc } }`.
// Everything else at the top level of storage is the cover for all other sites.

// Self-contained: background.js serializes it into the user script.
// The most specific rule wins, so a rule for example.com also covers shop.example.com.
// A rule from before sites had a mode of their own keeps All sites' mode.
export function coverFor(host, { sites = {}, ...everywhere }, now = Date.now()) {
  for (let h = host; h; h = h.slice(h.indexOf('.') + 1 || h.length)) {
    if (Object.hasOwn(sites, h) && (!sites[h].expiresAt || sites[h].expiresAt > now)) return { useDebuggerApi: everywhere.useDebuggerApi, ...sites[h], site: h }
  }
  return everywhere
}

// What a site's pages are given to run themselves (stealth.js): the cover in Stealth mode, and `relay`
// while the site seals WebRTC on its own and Chrome's browser-wide seal is off. Null when they need nothing.
export const pageCover = (settings, host) => {
  const { useDebuggerApi, sealWebRtc, configuration, timezone, locale, lat, lon } = coverFor(host, settings)
  const cover = useDebuggerApi ? {} : { configuration, timezone, locale, lat, lon }
  if (sealWebRtc && !settings.sealWebRtc) cover.relay = true
  return isActive(cover) || cover.relay ? cover : null
}

// The key a new rule is saved under: http(s) hostname without a leading "www.".
export const siteKey = (url) => {
  try {
    const { protocol, hostname } = new URL(url)
    return /^https?:$/.test(protocol) ? hostname.replace(/^www\./, '') : null
  } catch {
    return null
  }
}

// A cover's language preferences, most preferred first: 'ja-JP' -> ['ja-JP', 'ja'].
export const languagesFor = (locale) => [...new Set([locale, locale.split('-')[0]])]

export const hasCoordinates = ({ lat, lon }) => Number.isFinite(lat) && Number.isFinite(lon) && Math.abs(lat) <= 90 && Math.abs(lon) <= 180
export const isActive = (cover) => !!(cover.timezone || cover.locale || hasCoordinates(cover))

export const coordinates = (lat, lon) => {
  const number = (value) => value === null || value === undefined || value === '' ? null
    : typeof value === 'number' || typeof value === 'string' && value.trim() ? Number(value) : NaN
  const result = { lat: number(lat), lon: number(lon) }
  if (result.lat === null && result.lon === null) return result
  if (!hasCoordinates(result)) throw new Error('Enter both coordinates: latitude -90 to 90, longitude -180 to 180.')
  return result
}

export const coverKey = (cover) => JSON.stringify([cover.configuration, cover.timezone ?? null, cover.locale ?? null,
  cover.lat ?? null, cover.lon ?? null, !!cover.useDebuggerApi, !!cover.sealWebRtc])

// What the All sites cover asks of the computer's own clock, for a companion that can keep the system
// on Alibi's location (Whereabouts on the Mac, Alibi Companion on Windows): its kind, its zone, and the
// locale whose country the system's region can follow. Per-site covers stay in the browser: the system
// has one clock.
export const systemCover = ({ configuration, timezone, locale }) => {
  const on = configuration && configuration !== 'browserDefault'
  return {
    configuration: on ? configuration : 'browserDefault',
    timezone: on && timezone ? timezone : null,
    locale: on && locale ? locale : null,
  }
}

// Whether the All sites cover is a place, the only kind a companion moves the system's clock for. Off
// and "Match my IP address" leave the system to follow the connection, as it does without Alibi.
export const holdsPlace = (stored) => {
  const { configuration, timezone } = systemCover(stored)
  return configuration !== 'browserDefault' && configuration !== 'ipAddress' && !!timezone
}

// Whether two zone names are one zone as the browser names it: Asia/Kolkata and Asia/Calcutta are.
export const sameZone = (a, b) => {
  if (!a || !b) return false
  try {
    const named = (zone) => new Intl.DateTimeFormat('en-US', { timeZone: zone }).resolvedOptions().timeZone
    return named(a) === named(b)
  } catch {
    return false
  }
}

// "Match my IP address": where GeoJS places this connection, as a cover.
// GeoJS: free, no key, HTTPS, so the lookup can't be read or altered on the way.
export const ipCover = async () => {
  const response = await fetch('https://get.geojs.io/v1/ip/geo.json', { signal: AbortSignal.timeout(10000), cache: 'no-store', credentials: 'omit', redirect: 'error' })
  if (!response.ok) throw new Error(`GeoJS answered ${response.status}`)
  const ip = await response.json()
  // The country's most likely language, from CLDR: JP -> ja-JP, CH -> de-CH
  const { language, region } = new Intl.Locale(`und-${ip.country_code}`).maximize()
  return checkedCover({ timezone: ip.timezone, locale: `${language}-${region}`, lat: ip.latitude, lon: ip.longitude })
}

// A place from outside Alibi (GeoJS, Whereabouts) as a cover. A missing or unknown zone or locale
// would break pages, so it throws instead.
export const checkedCover = ({ timezone, locale, lat, lon }) => {
  if (typeof timezone !== 'string') throw new Error('No time zone')
  new Intl.DateTimeFormat('en-US', { timeZone: timezone })
  return { timezone, locale: new Intl.Locale(locale).toString(), ...coordinates(lat, lon) }
}

const followingIp = (rule) => rule?.configuration === 'ipAddress'
export const followsIp = ({ sites = {}, ...everywhere }) => [everywhere, ...Object.values(sites)].some(followingIp)

// The storage changes that move every "Match my IP address" cover to `ip`. Empty when they're all
// there already, so an unchanged connection writes nothing.
export const followIp = ({ sites = {}, ...everywhere }, ip) => {
  const stale = (rule) => followingIp(rule) && Object.keys(ip).some((key) => rule[key] !== ip[key])
  const changes = stale(everywhere) ? { ...ip } : {}
  if (Object.values(sites).some(stale)) {
    changes.sites = Object.fromEntries(Object.entries(sites).map(([site, rule]) => [site, stale(rule) ? { ...rule, ...ip } : rule]))
  }
  return changes
}

// Store builds leave the permission out: the Web Store reserves userScripts for user-script managers.
export const userScriptsPermitted = chrome.runtime.getManifest().permissions.includes('userScripts')

// chrome.userScripts only works once the user turns on "Allow User Scripts" for Alibi.
export const userScriptsAvailable = () => {
  try {
    chrome.userScripts.getScripts() // throws synchronously while the toggle is off
    return true
  } catch {
    return false
  }
}
