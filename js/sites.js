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
