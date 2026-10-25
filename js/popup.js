import { detachDebugger } from './debugger.js'
import locationsConfigurations from './locationsConfigurations.js'
import { coverFor, ipCover, sameZone, siteKey, userScriptsAvailable, userScriptsPermitted } from './sites.js'

const $ = (selector) => document.querySelector(selector)

const reloadButton = $('#reloadButton')
const reloadLabel = $('#reloadLabel')
const configurationSelect = $('select[name="configuration"]')
const locationsOptGroup = $('#locationsOptGroup')
const timeZoneInput = $('input[name="timeZone"]')
const localeInput = $('input[name="locale"]')
const latitudeInput = $('input[name="latitude"]')
const longitudeInput = $('input[name="longitude"]')
const fieldError = $('#fieldError')
const ipError = $('#ipError')
const scopeAllTab = $('#scopeAll')
const scopeSiteTab = $('#scopeSite')
const coverPanel = $('#coverPanel')
const coverFields = $('#coverFields')
const siteEmpty = $('#siteEmpty')
const removeSiteRuleButton = $('#removeSiteRule')
const stealthInput = $('input[name="mode"][value="stealth"]')
const deepInput = $('input[name="mode"][value="deep"]')
const modeNote = $('#modeNote')
const userScriptsHint = $('#userScriptsHint')
const die = $('#die')
const dieRing = $('#dieRing')
const dieCity = $('#dieCity')
const dieRegion = $('#dieRegion')
const dieDate = $('#dieDate')
const dieTime = $('#dieTime')
const dieOffset = $('#dieOffset')
const strikeLead = $('#strikeLead')
const strikePlace = $('#strikePlace')
const strikeMeta = $('#strikeMeta')

const realTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone
const regionNames = new Intl.DisplayNames(['en'], { type: 'region' })

// Add location options to the select menu
Object.entries(locationsConfigurations).forEach(([key, location]) => {
  const option = document.createElement('option')
  option.value = key
  option.textContent = location.name
  locationsOptGroup.appendChild(option)
})

// Only asked when "Match my IP address" is chosen, so opening the popup leaks nothing.
// From then on background.js keeps that cover following the connection.
let ipRequest = null
const fetchIpCover = () =>
  (ipRequest ??= ipCover().catch((error) => {
    ipRequest = null
    throw error
  }))

const isTimeZone = (value) => {
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: value })
    return true
  } catch {
    return false
  }
}
const isLocale = (value) => {
  try {
    return Intl.getCanonicalLocales(value).length > 0
  } catch {
    return false
  }
}

// "America/Argentina/Buenos_Aires" -> "Buenos Aires"
const cityFromZone = (zone) => zone.split('/').pop().replaceAll('_', ' ')
const regionFromLocale = (locale) => {
  try {
    return new Intl.Locale(locale).maximize().region ?? null
  } catch {
    return null
  }
}
const countryFromLocale = (locale) => {
  const region = regionFromLocale(locale)
  return region ? regionNames.of(region) : null
}

// --- State: a mirror of chrome.storage.local, and which record the fields edit ----------------

let stored = {}
const [activeTab] = await chrome.tabs.query({ active: true, currentWindow: true })
const tabHost = (() => {
  try {
    return new URL(activeTab?.url).hostname
  } catch {
    return ''
  }
})()
let scope = 'all' // or 'site'
let ruleKey = siteKey(activeTab?.url ?? '') // null when the tab isn't a website

const siteRule = () => (ruleKey ? stored.sites?.[ruleKey] : undefined)

// "52,52" and "52.52" both mean 52.52; anything else is NaN. Empty is null.
const parseCoordinate = (value) => {
  const text = value.trim().replace(',', '.')
  return text ? (/^[+-]?\d+(\.\d+)?$/.test(text) ? Number(text) : NaN) : null
}

const readFields = () => ({
  configuration: configurationSelect.value,
  timezone: timeZoneInput.value.trim() || null,
  locale: localeInput.value.trim() || null,
  lat: parseCoordinate(latitudeInput.value) || null,
  lon: parseCoordinate(longitudeInput.value) || null,
})
const setInputs = (timezone, locale, lat, lon) => {
  timeZoneInput.value = timezone || ''
  localeInput.value = locale || ''
  latitudeInput.value = lat || ''
  longitudeInput.value = lon || ''
}
const clearInputs = () => setInputs('', '', '', '')
const writeFields = (record = {}) => {
  configurationSelect.value = record.configuration || 'browserDefault'
  setInputs(record.timezone, record.locale, record.lat, record.lon)
}

// Storage as it will be once the fields are saved, so the strike previews edits as they're typed.
const preview = () => {
  if (scope === 'site') return siteRule() ? { ...stored, sites: { ...stored.sites, [ruleKey]: readFields() } } : stored
  return { ...stored, ...readFields() }
}

// --- The strike: what this tab's site currently sees ---------------------------------------

let strikeZone = realTimeZone
let lastStrikeKey = null

const stampParts = (zone) => {
  const parts = {}
  const format = new Intl.DateTimeFormat('en-US', {
    timeZone: zone, day: '2-digit', month: 'short', year: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZoneName: 'longOffset',
  })
  for (const { type, value } of format.formatToParts(new Date())) parts[type] = value
  return {
    date: `${parts.day} ${parts.month.toUpperCase()} ${parts.year}`,
    time: `${parts.hour}:${parts.minute}`,
    offset: parts.timeZoneName === 'GMT' ? 'UTC±00:00' : parts.timeZoneName.replace('GMT', 'UTC'),
  }
}

const updateClock = () => {
  const { date, time, offset } = stampParts(strikeZone)
  dieDate.textContent = date
  dieTime.textContent = time
  dieOffset.textContent = offset
}

// How the popup names this computer when its own clock is on the cover's zone too.
const platform = navigator.userAgentData?.platform ?? navigator.platform
const onSystem = /mac/i.test(platform) ? 'on your Mac too' : /win/i.test(platform) ? 'on Windows too' : 'on this computer too'

const describe = (cover) => {
  const zone = cover.timezone && isTimeZone(cover.timezone) ? cover.timezone : null
  const locale = cover.locale && isLocale(cover.locale) ? cover.locale : null
  const preset = locationsConfigurations[cover.configuration]
  const who = cover.site ?? 'Sites'
  const sees = cover.site ? 'sees' : 'see'

  if (!(zone || locale || cover.lat || cover.lon)) {
    return {
      muted: true, zone: realTimeZone, city: cityFromZone(realTimeZone), region: 'Real location',
      lead: `${who} ${sees} your real location`, extras: [navigator.language],
    }
  }
  const lead = `${who} ${sees} you in`
  // A companion holds the computer's own clock on this zone too (background.js, systemHeld).
  const system = zone && sameZone(zone, stored.systemZone) ? onSystem : null
  if (preset) {
    const [city, country = ''] = preset.name.split(', ')
    return { zone: zone ?? realTimeZone, city, region: country || city, locale, lead, extras: [locale, system].filter(Boolean) }
  }
  const country = (locale && countryFromLocale(locale)) || (zone ? zone.split('/')[0] : null)
  const coords = [cover.lat, cover.lon].filter(Boolean).join(', ')
  return {
    zone: zone ?? realTimeZone, city: zone ? cityFromZone(zone) : 'Custom cover', region: country ?? 'Custom', locale, lead,
    extras: [locale, coords, system].filter(Boolean),
  }
}

const renderStrike = (override) => {
  // While a field holds a typo, keep showing what sites actually get: the last saved cover.
  const cover = { ...describe(coverFor(tabHost, problems().length ? stored : preview())), ...override }
  strikeZone = cover.zone
  strikeLead.textContent = cover.lead
  strikePlace.textContent = cover.city
  strikeMeta.textContent = override?.meta ?? [stampParts(cover.zone).offset, ...cover.extras].join(' · ')

  const cityText = cover.city.toUpperCase()
  dieCity.textContent = cityText
  dieCity.setAttribute('font-size', String(Math.max(9, Math.min(13, 13 - (cityText.length - 9) * 0.5))))
  const code = !cover.muted && cover.locale ? regionFromLocale(cover.locale) : null
  const regionText = (code && cover.region !== code ? `${cover.region} · ${code}` : cover.region).toUpperCase()
  dieRegion.textContent = regionText
  dieRegion.setAttribute('font-size', String(Math.max(7.5, Math.min(9.5, 9.5 - (regionText.length - 14) * 0.25))))
  die.classList.toggle('text-ink', !cover.muted)
  die.classList.toggle('text-ink3', !!cover.muted)
  dieRing.setAttribute('stroke-dasharray', cover.muted ? '5 4' : 'none')
  updateClock()

  const key = `${cover.city}|${cover.zone}|${cover.muted}`
  if (key === lastStrikeKey) return
  // Re-strike: a copy of the old impression lifts off while the new die presses down after it.
  const lifting = lastStrikeKey !== null && !matchMedia('(prefers-reduced-motion: reduce)').matches
  if (lifting) {
    const ghost = die.cloneNode(true)
    ghost.querySelectorAll('[id]').forEach((el) => el.removeAttribute('id'))
    ghost.removeAttribute('id')
    ghost.classList.remove('die-strike')
    ghost.classList.add('die-lift')
    ghost.style.left = `${die.offsetLeft}px`
    ghost.style.top = `${die.offsetTop}px`
    ghost.addEventListener('animationend', () => ghost.remove())
    die.after(ghost)
  }
  lastStrikeKey = key
  // Each place gets its own slight, repeatable tilt, like a hand-struck die.
  const hash = [...key].reduce((h, c) => (h * 31 + c.charCodeAt(0)) | 0, 7)
  die.style.setProperty('--die-rot', `${(Math.abs(hash) % 11) - 7}deg`)
  die.style.setProperty('--strike-delay', lifting ? '90ms' : '0ms')
  die.classList.remove('die-strike')
  void die.getBoundingClientRect() // restart the strike animation
  die.classList.add('die-strike')
}

// --- Scope tabs ------------------------------------------------------------------------------

const renderScope = () => {
  scopeAllTab.setAttribute('aria-selected', String(scope === 'all'))
  scopeSiteTab.setAttribute('aria-selected', String(scope === 'site'))
  scopeAllTab.tabIndex = scope === 'all' ? 0 : -1
  scopeSiteTab.tabIndex = scope === 'site' ? 0 : -1
  coverPanel.setAttribute('aria-labelledby', scope === 'all' ? 'scopeAll' : 'scopeSite')
  $('#scopeSiteName').textContent = ruleKey ?? 'This site'
  scopeSiteTab.disabled = !ruleKey
  scopeSiteTab.title = ruleKey ? `Settings for ${ruleKey} and its subdomains` : 'Open a website to give it its own cover'
  const empty = scope === 'site' && !siteRule()
  siteEmpty.hidden = !empty
  coverFields.hidden = empty
  removeSiteRuleButton.hidden = scope !== 'site' || empty
  $('#siteEmptyName').textContent = ruleKey ?? ''
}

const selectScope = (next) => {
  scope = next
  writeFields(scope === 'site' ? siteRule() : stored)
  validate()
  renderScope()
  renderStrike()
}

// --- Mode ----------------------------------------------------------------------------------

const renderMode = () => {
  modeNote.textContent = deepInput.checked ? 'Chrome shows a debugging banner.' : 'No browser banner.'
  updateHint()
}

// One notice at a time: an error outranks the User Scripts hint, which returns once it's fixed.
const updateHint = () => {
  userScriptsHint.hidden =
    !userScriptsPermitted || deepInput.checked || userScriptsAvailable() || !fieldError.hidden || !ipError.hidden
}

// --- Saving ----------------------------------------------------------------------------------

// Each problem names its field so validate() can mark it.
const problems = () => {
  if (coverFields.hidden) return []
  const zone = timeZoneInput.value.trim()
  const locale = localeInput.value.trim()
  const lat = parseCoordinate(latitudeInput.value)
  const lon = parseCoordinate(longitudeInput.value)
  const found = []
  if (zone && !isTimeZone(zone)) found.push([timeZoneInput, `“${zone}” isn’t a time zone. Try a name like Europe/Berlin.`])
  if (locale && !isLocale(locale)) found.push([localeInput, `“${locale}” isn’t a locale. Try a tag like de-DE.`])
  if (lat !== null && !(Math.abs(lat) <= 90)) found.push([latitudeInput, 'Latitude runs from -90 to 90.'])
  if (lon !== null && !(Math.abs(lon) <= 180)) found.push([longitudeInput, 'Longitude runs from -180 to 180.'])
  return found
}

const validate = () => {
  const found = problems()
  for (const input of [timeZoneInput, localeInput, latitudeInput, longitudeInput]) {
    input.setAttribute('aria-invalid', String(found.some(([field]) => field === input)))
  }
  fieldError.textContent = found.map(([, message]) => message).join(' ')
  fieldError.hidden = found.length === 0
  updateHint()
  return found.length === 0
}

const markPending = (pending) => {
  reloadButton.dataset.pending = String(pending)
  reloadLabel.textContent = pending ? 'Reload to apply' : 'Reload tab'
}

const persist = async (changes) => {
  Object.assign(stored, changes)
  detachDebugger()
  await chrome.storage.local.set(changes)
  markPending(true)
}

const saveToStorage = () => {
  renderStrike()
  // Keep the last good settings until a typo is fixed; a bad zone or locale would break pages.
  if (!validate()) return
  if (scope === 'site') return persist({ sites: { ...stored.sites, [ruleKey]: readFields() } })
  return persist(readFields())
}

const handleConfigurationChange = async () => {
  const configuration = configurationSelect.value
  ipError.hidden = true
  updateHint()

  if (configuration === 'browserDefault' || configuration === 'custom') {
    clearInputs()
  } else if (configuration === 'ipAddress') {
    // Nothing is saved until the lookup lands, so the strike keeps the cover that still applies.
    renderStrike({ lead: 'Looking up your IP address…', meta: 'Asking GeoJS' })
    try {
      const ip = await fetchIpCover()
      if (configurationSelect.value !== 'ipAddress') return // the user moved on while we waited
      setInputs(ip.timezone, ip.locale, ip.lat, ip.lon)
    } catch {
      configurationSelect.value = (scope === 'site' ? siteRule() : stored)?.configuration || 'browserDefault'
      renderStrike()
      ipError.hidden = false
      updateHint()
      return
    }
  } else {
    const selectedLocation = locationsConfigurations[configuration]
    setInputs(selectedLocation.timezone, selectedLocation.locale, selectedLocation.lat, selectedLocation.lon)
  }

  saveToStorage()
}

// Debounce function to limit frequent save calls
const debounce = (func, delay) => {
  let timeoutId
  return (...args) => {
    clearTimeout(timeoutId)
    timeoutId = setTimeout(() => func(...args), delay)
  }
}

const debouncedSaveToStorage = debounce(saveToStorage, 300)

const handleInputChange = () => {
  configurationSelect.value = 'custom'
  validate()
  renderStrike()
  debouncedSaveToStorage()
}

// --- Wiring ----------------------------------------------------------------------------------

reloadButton.addEventListener('click', () => {
  chrome.tabs.reload()
  markPending(false)
})
$('#infoButton').addEventListener('click', () => chrome.tabs.create({ url: 'html/info.html' }))
$('#ipRetry').addEventListener('click', () => {
  configurationSelect.value = 'ipAddress'
  handleConfigurationChange()
})
configurationSelect.addEventListener('change', handleConfigurationChange)
timeZoneInput.addEventListener('input', handleInputChange)
localeInput.addEventListener('input', handleInputChange)
latitudeInput.addEventListener('input', handleInputChange)
longitudeInput.addEventListener('input', handleInputChange)

scopeAllTab.addEventListener('click', () => selectScope('all'))
scopeSiteTab.addEventListener('click', () => selectScope('site'))
for (const tab of [scopeAllTab, scopeSiteTab]) {
  tab.addEventListener('keydown', (event) => {
    if (!['ArrowLeft', 'ArrowRight'].includes(event.key) || scopeSiteTab.disabled) return
    const next = scope === 'all' ? 'site' : 'all'
    selectScope(next)
    ;(next === 'all' ? scopeAllTab : scopeSiteTab).focus()
  })
}
$('#addSiteRule').addEventListener('click', async () => {
  const { configuration, timezone, locale, lat, lon } = stored
  await persist({ sites: { ...stored.sites, [ruleKey]: { configuration, timezone, locale, lat, lon } } })
  selectScope('site')
  configurationSelect.focus()
})
removeSiteRuleButton.addEventListener('click', async () => {
  const { [ruleKey]: _removed, ...sites } = stored.sites ?? {}
  await persist({ sites })
  selectScope('site')
  $('#addSiteRule').focus()
})

for (const input of [stealthInput, deepInput]) {
  input.addEventListener('change', () => {
    renderMode()
    persist({ useDebuggerApi: deepInput.checked })
  })
}
$('#userScriptsLink').addEventListener('click', () =>
  chrome.tabs.create({ url: `chrome://extensions/?id=${chrome.runtime.id}` })
)

// --- Start -------------------------------------------------------------------------------------

// A companion that has started following Alibi's location since hears about the place now (background.js).
chrome.runtime.sendMessage({ checkCompanion: true }).catch(() => {})
stored = await chrome.storage.local.get(['configuration', 'timezone', 'locale', 'lat', 'lon', 'useDebuggerApi', 'sites', 'systemZone'])
// On shop.example.com, an existing rule for example.com is the one to show and edit.
const matched = coverFor(tabHost, stored).site
if (matched) {
  ruleKey = matched
  scope = 'site'
}
;(stored.useDebuggerApi ? deepInput : stealthInput).checked = true
writeFields(scope === 'site' ? siteRule() : stored)
validate()
renderScope()
renderMode()
renderStrike()
setInterval(updateClock, 1000)
// The companion says it holds the system's clock a moment after a change: show it when it does.
chrome.storage.onChanged.addListener(({ systemZone }) => {
  if (!systemZone) return
  stored.systemZone = systemZone.newValue
  renderStrike()
})
