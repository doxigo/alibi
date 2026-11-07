import locations from './locationsConfigurations.js'
import { languagesFor } from './sites.js'
import { checkedImport, checkedSettingsCover, checkedSite, exportSettings, lookupIp, saveSettings, SETTINGS_KEYS } from './settings.js'
import diagnose from './diagnostics.js'

const $ = (selector) => document.querySelector(selector)
let stored = await chrome.storage.local.get(SETTINGS_KEYS)
let selected = null
let configuration = 'custom'
let expiry = null
let dirty = false
let revision = 0
let checking = false
let diagnosticRevision = 0
const invalidateDiagnostics = () => {
  diagnosticRevision++
  $('#results').hidden = true
}
const notice = (text, error = false) => {
  $('#notice').textContent = text
  $('#notice').dataset.error = String(error)
  $('#notice').hidden = false
  if (error) $('#notice').focus()
}
const run = (job) => async (event) => {
  event?.preventDefault()
  try { await job(event) } catch (error) { notice(error.message, true) }
}
const save = async (action) => {
  stored = await saveSettings(action)
  renderLists()
  refreshStatus()
}
const fields = () => {
  const number = (id) => $(id).value.trim() === '' ? null : Number($(id).value.trim().replace(',', '.'))
  const cover = { configuration, timezone: $('#timezone').value.trim() || null, locale: $('#locale').value.trim() || null,
    lat: number('#lat'), lon: number('#lon'), useDebuggerApi: $('#mode').value === 'deep', sealWebRtc: $('#seal').checked }
  if (selected) cover.expiresAt = $('#expiry').value === 'keep' ? expiry : Number($('#expiry').value) ? Date.now() + Number($('#expiry').value) * 60000 : null
  return { ...checkedSettingsCover(cover, !!selected), ...(selected ? { expiresAt: cover.expiresAt } : {}) }
}
const showCover = (cover) => {
  revision++
  configuration = cover.configuration ?? 'custom'
  for (const key of ['timezone', 'locale', 'lat', 'lon']) $(`#${key}`).value = cover[key] ?? ''
  $('#mode').value = cover.useDebuggerApi ? 'deep' : 'stealth'
  $('#seal').checked = !!cover.sealWebRtc
  expiry = cover.expiresAt ?? null
  $('#expiry').value = 'keep'
}
const selectRule = (site) => {
  selected = site
  dirty = false
  $('#editorTitle').textContent = site ?? 'All sites'
  $('#scopeNote').textContent = site ? 'Applies to this site and its subdomains. More specific rules win.' : 'Used wherever no site rule applies.'
  $('#expiry').disabled = !site
  $('#removeRule').hidden = !site
  showCover(site ? { useDebuggerApi: stored.useDebuggerApi, ...stored.sites[site] } : exportSettings(stored).cover)
  $('#preset').value = ''
  renderLists()
}
const renderLists = () => {
  const query = $('#search').value.toLowerCase()
  const focused = $('#rules').contains(document.activeElement) ? document.activeElement.dataset.site : null
  $('#rules').replaceChildren()
  for (const site of [null, ...Object.keys(stored.sites ?? {}).sort()]) {
    if (site && !site.toLowerCase().includes(query)) continue
    const button = document.createElement('button')
    button.type = 'button'
    button.dataset.site = site ?? ''
    const expires = site && stored.sites[site].expiresAt
    button.textContent = (site ?? 'All sites') + (expires ? ` (until ${new Date(expires).toLocaleString()})` : '')
    button.setAttribute('aria-current', String(site === selected))
    button.addEventListener('click', () => {
      if (dirty) return notice('Save your changes before selecting another cover, or reload this manager to discard them.', true)
      selectRule(site)
    })
    $('#rules').append(button)
    if (focused === button.dataset.site) button.focus({ preventScroll: true })
  }
  const chosen = $('#profiles').value
  $('#profiles').replaceChildren(new Option('Choose a profile', ''))
  for (const name of Object.keys(stored.profiles ?? {}).sort()) $('#profiles').add(new Option(name, name))
  $('#profiles').value = chosen
  $('#loadProfile').disabled = $('#removeProfile').disabled = !$('#profiles').value
}
for (const [key, place] of Object.entries(locations)) $('#preset').add(new Option(place.name, key))
$('#search').addEventListener('input', renderLists)
$('#profiles').addEventListener('change', renderLists)
$('#editor').addEventListener('input', (event) => {
  revision++
  dirty = true
  if (['timezone', 'locale', 'lat', 'lon'].includes(event.target.id)) configuration = 'custom'
})
$('#preset').addEventListener('change', run(async () => {
  const choice = $('#preset').value
  if (!choice) return
  const site = selected
  $('#preset').disabled = true
  try {
    const cover = choice === 'ipAddress' ? await lookupIp() : locations[choice] ?? {}
    if (selected !== site) return
    const duration = $('#expiry').value
    showCover({ ...cover, configuration: choice, useDebuggerApi: $('#mode').value === 'deep', sealWebRtc: $('#seal').checked, expiresAt: expiry })
    $('#expiry').value = duration
    dirty = true
  } finally { $('#preset').disabled = false }
}))
$('#editor').addEventListener('submit', run(async () => {
  const cover = fields()
  const site = selected, savedRevision = revision
  await save({ command: 'cover', site, changes: cover })
  if (selected === site && revision === savedRevision) selectRule(site)
  notice('Cover saved. Open tabs show whether a reload is needed.')
}))
$('#addRule').addEventListener('submit', run(async () => {
  if (dirty) throw new Error('Save your current edits before adding a site.')
  const site = checkedSite($('#newSite').value)
  if (!Object.hasOwn(stored.sites ?? {}, site)) await save({ command: 'cover', site, changes: exportSettings(stored).cover })
  selectRule(site)
  $('#newSite').value = ''
  $('#timezone').focus()
}))
$('#removeRule').addEventListener('click', run(async () => {
  await save({ command: 'removeSite', site: selected })
  selectRule(null)
  notice('Site rule removed. It now uses the next matching rule or All sites.')
}))
$('#saveProfile').addEventListener('submit', run(async () => {
  const { expiresAt, ...cover } = fields()
  const name = $('#profileName').value.trim()
  await save({ command: 'profile', name, cover })
  $('#profiles').value = name
  renderLists()
  notice('Profile saved.')
}))
$('#loadProfile').addEventListener('click', () => {
  const profile = stored.profiles?.[$('#profiles').value]
  if (!profile) return
  showCover(profile)
  dirty = true
  notice('Profile loaded into the form. Save cover to apply it.')
})
$('#removeProfile').addEventListener('click', run(async () => {
  await save({ command: 'removeProfile', name: $('#profiles').value })
  notice('Profile deleted. Existing site covers are unchanged.')
}))
$('#export').addEventListener('click', () => {
  const url = URL.createObjectURL(new Blob([JSON.stringify(exportSettings(stored), null, 2)], { type: 'application/json' }))
  const link = document.createElement('a')
  link.href = url
  link.download = 'alibi-settings.json'
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
})
$('#import').addEventListener('click', () => $('#importFile').click())
$('#importFile').addEventListener('change', run(async () => {
  const file = $('#importFile').files[0]
  $('#importFile').value = ''
  if (!file) return
  if (dirty) throw new Error('Save your edits before importing a backup.')
  if (file.size > 1024 * 1024) throw new Error('Backups must be smaller than 1 MB.')
  const data = JSON.parse(await file.text())
  checkedImport(data)
  await save({ command: 'import', data })
  selectRule(null)
  notice('Backup imported and merged. Matching names and All sites were replaced.')
}))

const refreshStatus = async () => {
  const tabId = Number($('#tabs').value)
  $('#checkTab').disabled = !tabId || checking
  $('#reloadTab').disabled = !tabId
  if (!tabId) return
  const answer = await chrome.runtime.sendMessage({ status: tabId })
  if (Number($('#tabs').value) !== tabId) return answer
  if (!answer?.ok) return $('#appliedStatus').textContent = answer?.error ?? 'Status unavailable.'
  $('#appliedStatus').textContent = `${answer.status.state.replaceAll('-', ' ')}: ${answer.status.detail ?? ''}`
  const ip = answer.ipStatus
  $('#ipStatus').textContent = ip?.lastSuccess ? `Last location update: ${new Date(ip.lastSuccess).toLocaleString()} (${ip.source}).${ip.error ? ` Latest check failed: ${ip.error}` : ''}` : 'No successful IP lookup recorded yet.'
  return answer
}
const initial = new URL(location.href).searchParams.get('tab')
const refreshTabs = async () => {
  const chosen = $('#tabs').value || initial
  const tabs = (await chrome.tabs.query({})).filter((tab) => /^https?:/.test(tab.url ?? ''))
  $('#tabs').replaceChildren(...tabs.map((tab) => new Option(`${new URL(tab.url).hostname} (tab ${tab.id})`, String(tab.id))))
  if (tabs.some((tab) => String(tab.id) === chosen)) $('#tabs').value = chosen
  if (!tabs.length) {
    $('#tabs').add(new Option('No website tabs open', ''))
    $('#appliedStatus').textContent = 'Open a website to check its cover.'
  }
  refreshStatus()
}
chrome.tabs.onCreated.addListener(refreshTabs)
chrome.tabs.onRemoved.addListener((tabId) => {
  if (tabId === Number($('#tabs').value)) invalidateDiagnostics()
  refreshTabs()
})
chrome.tabs.onUpdated.addListener((tabId, change) => {
  if (tabId === Number($('#tabs').value) && (change.url || change.status === 'loading')) invalidateDiagnostics()
  if (change.url || change.status === 'complete') refreshTabs()
})
await refreshTabs()
$('#tabs').addEventListener('change', () => { invalidateDiagnostics(); refreshStatus() })
$('#reloadTab').addEventListener('click', run(async () => { await chrome.tabs.reload(Number($('#tabs').value)); notice('Tab reloading.') }))
$('#checkTab').addEventListener('click', run(async () => {
  invalidateDiagnostics()
  const revision = diagnosticRevision
  const tabId = Number($('#tabs').value)
  checking = true
  $('#checkTab').disabled = true
  $('#checkTab').textContent = 'Checking...'
  let timer
  try {
    const answer = await refreshStatus()
    if (!answer?.ok) throw new Error(answer?.error ?? 'Status unavailable.')
    const expected = answer.cover
    const [{ result, documentId }] = await Promise.race([
      chrome.scripting.executeScript({ target: { tabId }, world: 'MAIN', func: diagnose }),
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('The page did not finish its checks. Reload it and try again.')), 8000) }),
    ])
    const frame = await chrome.webNavigation.getFrame({ tabId, frameId: 0 })
    if (revision !== diagnosticRevision || Number($('#tabs').value) !== tabId || frame?.documentId !== documentId) {
      notice('The tab or its cover changed. Run diagnostics again.')
      return
    }
    if (!result) throw new Error('This page did not return diagnostic results.')
    const tbody = $('#results tbody')
    $('#results caption').textContent = `${new URL(frame.url).hostname}, checked ${new Date().toLocaleString()}. A match does not prove anonymity.`
    tbody.replaceChildren()
    const row = (label, want, actual) => {
      const tr = document.createElement('tr')
      for (const text of [label, want, actual]) { const td = document.createElement('td'); td.textContent = text; tr.append(td) }
      tbody.append(tr)
    }
    for (const name of ['page', 'frame', 'classic', 'module']) {
      const observed = result[name]
      row(`${name}: time zone / locale / language`, `${expected.timezone ?? 'Browser default'} / ${expected.locale ?? 'Browser default'}`, observed?.error ?? `${observed?.timezone} / ${observed?.locale} / ${observed?.language}`)
    }
    row('Accept-Language header', expected.locale ? languagesFor(expected.locale).join(', ') : 'Browser default', result.headers.error ?? result.headers.language)
    row('Geolocation', expected.lat == null ? 'Browser default' : `${expected.lat}, ${expected.lon}`, result.geolocation.error ?? `${result.geolocation.latitude}, ${result.geolocation.longitude}`)
    row('WebRTC candidates', 'Inspect for unexpected addresses', result.webrtc.error ?? (result.webrtc.addresses.join(', ') || 'No candidates; inconclusive'))
    $('#results').hidden = false
    notice('Diagnostics finished. Blocked and timed-out checks remain unverified.')
  } finally { clearTimeout(timer); checking = false; $('#checkTab').disabled = !Number($('#tabs').value); $('#checkTab').textContent = 'Run diagnostics' }
}))
chrome.storage.onChanged.addListener((changes, area) => {
  if (area === 'local' && SETTINGS_KEYS.some((key) => key !== 'profiles' && Object.hasOwn(changes, key))) invalidateDiagnostics()
  if (area === 'session' && Object.hasOwn(changes, `protection-${$('#tabs').value}`)) invalidateDiagnostics()
  if (area === 'local') {
    for (const [key, value] of Object.entries(changes)) {
      if (value.newValue === undefined) delete stored[key]
      else stored[key] = value.newValue
    }
    renderLists()
    if (!dirty) selectRule(selected && Object.hasOwn(stored.sites ?? {}, selected) ? selected : null)
  }
  refreshStatus()
})
selectRule(null)
refreshStatus()
