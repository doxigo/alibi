// Per-site covers live in storage as `sites: { 'example.com': { configuration, timezone, locale, lat, lon } }`.
// Everything else at the top level of storage is the cover for all other sites.

// Self-contained: background.js serializes it into the user script.
// The most specific rule wins, so a rule for example.com also covers shop.example.com.
export function coverFor(host, { sites = {}, ...everywhere }) {
  for (let h = host; h; h = h.slice(h.indexOf('.') + 1 || h.length)) {
    if (sites[h]) return { ...sites[h], site: h }
  }
  return everywhere
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

export const isActive = ({ timezone, locale, lat, lon }) => !!(timezone || locale || lat || lon)

// "Match my IP address": where GeoJS places this connection, as a cover.
// GeoJS: free, no key, HTTPS, so the lookup can't be read or altered on the way.
export const ipCover = async () => {
  const response = await fetch('https://get.geojs.io/v1/ip/geo.json')
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
  return { timezone, locale: new Intl.Locale(locale).toString(), lat: Number(lat) || null, lon: Number(lon) || null }
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
