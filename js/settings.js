import { coordinates, followIp, siteKey } from './sites.js'

export const COVER_FIELDS = ['configuration', 'timezone', 'locale', 'lat', 'lon', 'useDebuggerApi', 'sealWebRtc', 'expiresAt']
export const SETTINGS_KEYS = [...COVER_FIELDS.filter((key) => key !== 'expiresAt'), 'sites', 'profiles']
const object = (value) => value && typeof value === 'object' && !Array.isArray(value)
const limited = (value, max, label) => {
  if (typeof value !== 'string' || !value.trim() || value.length > max) throw new Error(`Invalid ${label}.`)
  return value.trim()
}

export const checkedSite = (value) => {
  const input = limited(value, 2048, 'site')
  const site = siteKey(input.includes('://') ? input : `https://${input}`)
  if (!site || site.length > 253 || /[\s*]/.test(site)) throw new Error('Enter a website hostname or an HTTP(S) URL.')
  return site
}

export const checkedSettingsCover = (value, temporary = false) => {
  if (!object(value)) throw new Error('Invalid cover.')
  for (const key of Object.keys(value)) if (!COVER_FIELDS.includes(key)) throw new Error(`Unknown cover field: ${key}`)
  const configuration = limited(value.configuration ?? 'custom', 80, 'configuration')
  let timezone = value.timezone ?? null
  let locale = value.locale ?? null
  if (timezone !== null) {
    timezone = limited(timezone, 128, 'time zone')
    new Intl.DateTimeFormat('en-US', { timeZone: timezone })
  }
  if (locale !== null) locale = new Intl.Locale(limited(locale, 128, 'locale')).toString()
  const cover = { configuration, timezone, locale, ...coordinates(value.lat, value.lon) }
  for (const key of ['useDebuggerApi', 'sealWebRtc']) {
    if (value[key] !== undefined && typeof value[key] !== 'boolean') throw new Error(`Invalid ${key}.`)
    if (value[key] !== undefined) cover[key] = value[key]
  }
  if (configuration === 'browserDefault') Object.assign(cover, { timezone: null, locale: null, lat: null, lon: null })
  if (value.expiresAt != null) {
    if (!temporary || !Number.isSafeInteger(value.expiresAt) || value.expiresAt <= 0) throw new Error('Invalid expiry time.')
    cover.expiresAt = value.expiresAt
  }
  return cover
}

export const exportSettings = (stored) => ({
  version: 1,
  cover: checkedSettingsCover(Object.fromEntries(COVER_FIELDS.filter((key) => key !== 'expiresAt' && stored[key] !== undefined).map((key) => [key, stored[key]]))),
  sites: stored.sites ?? {}, profiles: stored.profiles ?? {},
})

export const checkedImport = (value) => {
  if (!object(value) || value.version !== 1 || Object.keys(value).some((key) => !['version', 'cover', 'sites', 'profiles'].includes(key))) throw new Error('Unsupported Alibi backup. Expected version 1.')
  const result = { ...checkedSettingsCover(value.cover), sites: {}, profiles: {} }
  for (const kind of ['sites', 'profiles']) {
    const entries = value[kind] ?? {}
    if (!object(entries) || Object.keys(entries).length > (kind === 'sites' ? 1000 : 100)) throw new Error(`Too many or invalid ${kind}.`)
    result[kind] = Object.fromEntries(Object.entries(entries).map(([name, cover]) => [
      kind === 'sites' ? checkedSite(name) : limited(name, 80, 'profile name'), checkedSettingsCover(cover, kind === 'sites'),
    ]))
  }
  return result
}

export const settingsChange = (stored, action, now = Date.now()) => {
  if (!object(action)) throw new Error('Invalid settings request.')
  const sites = { ...stored.sites }
  switch (action.command) {
    case 'cover': {
      const site = action.site == null ? null : checkedSite(action.site)
      const previous = site && Object.hasOwn(sites, site) ? sites[site] : exportSettings(stored).cover
      if (!object(action.changes)) throw new Error('Invalid cover changes.')
      const cover = checkedSettingsCover({ ...previous, ...action.changes }, !!site)
      if (!site) return cover
      const next = { ...sites, [site]: cover }
      if (Object.keys(next).length > 1000) throw new Error('At most 1,000 site rules are supported.')
      return { sites: next }
    }
    case 'removeSite':
      delete sites[checkedSite(action.site)]
      return { sites }
    case 'profile': {
      const name = limited(action.name, 80, 'profile name')
      const profiles = { ...stored.profiles, [name]: checkedSettingsCover(action.cover) }
      if (Object.keys(profiles).length > 100) throw new Error('At most 100 profiles are supported.')
      return { profiles }
    }
    case 'removeProfile': {
      const profiles = { ...stored.profiles }
      delete profiles[limited(action.name, 80, 'profile name')]
      return { profiles }
    }
    case 'import': {
      const imported = checkedImport(action.data)
      imported.sites = { ...sites, ...imported.sites }
      imported.profiles = { ...stored.profiles, ...imported.profiles }
      if (Object.keys(imported.sites).length > 1000 || Object.keys(imported.profiles).length > 100) throw new Error('The merged backup exceeds the rule or profile limit.')
      return imported
    }
    case 'expire':
      return { sites: Object.fromEntries(Object.entries(sites).filter(([, rule]) => !rule.expiresAt || rule.expiresAt > now)) }
    case 'ip':
      return followIp(stored, action.cover)
    default:
      throw new Error('Unknown settings request.')
  }
}

export const settingsWriter = (storage) => {
  let pending = Promise.resolve()
  return (action) => {
    const write = pending.then(async () => {
      const stored = await storage.get(SETTINGS_KEYS)
      const changes = settingsChange(stored, action)
      if (Object.keys(changes).some((key) => JSON.stringify(changes[key]) !== JSON.stringify(stored[key]))) await storage.set(changes)
      return { ...stored, ...changes }
    })
    pending = write.catch(() => {})
    return write
  }
}

export const saveSettings = async (action) => {
  const answer = await chrome.runtime.sendMessage({ settings: action })
  if (!answer?.ok) throw new Error(answer?.error ?? 'Could not save settings. Try again.')
  return answer.stored
}

export const lookupIp = async () => {
  const answer = await chrome.runtime.sendMessage({ lookupIp: true })
  if (!answer?.ok) throw new Error(answer?.error ?? 'Could not look up your IP address.')
  return answer.cover
}
