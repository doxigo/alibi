import assert from 'node:assert/strict'
import { once } from 'node:events'
import { mkdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { browserSession } from './browser-session.mjs'
import { createTestServer } from './serve.mjs'

const server = createTestServer().listen(0, '127.0.0.1')
await once(server, 'listening')
const url = `http://localhost:${server.address().port}/browser.html`
let browser
const waitFor = async (read, label) => {
  const deadline = Date.now() + 10000
  let value
  while (Date.now() < deadline) {
    try { value = await read() }
    catch (error) {
      if (!/Frame with ID \d+ was removed\./.test(error.message)) throw error
      value = false
    }
    if (value) return value
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  throw new Error(`Timed out waiting for ${label}`)
}
try {
  browser = await browserSession()
  console.log(browser.version)
  const { sessionId: manager } = await browser.page(`chrome-extension://${browser.extensionId}/html/manage.html`)
  const evaluate = (expression) => browser.evaluate(manager, expression)
  await waitFor(() => evaluate('!!document.querySelector("#rules button")'), 'manager')
  const settings = async (action) => {
    const result = await evaluate(`chrome.runtime.sendMessage({settings:${JSON.stringify(action)}})`)
    assert.ok(result.ok, result.error)
    return result.stored
  }
  const read = (tabId, expression = 'window.check') => evaluate(`chrome.scripting.executeScript({target:{tabId:${tabId}},world:'MAIN',func:()=>(${expression})}).then(([r])=>r.result)`)
  const status = (tabId) => evaluate(`chrome.runtime.sendMessage({status:${tabId}})`)
  const loaded = async (tabId) => waitFor(async () => {
    try {
      const result = await read(tabId)
      return result?.cross && result.same && result.blob && result.file && result.module ? result : false
    } catch { return false }
  }, `tab ${tabId} surfaces`)
  const tokyo = { configuration: 'custom', timezone: 'Asia/Tokyo', locale: 'ja-JP', lat: 0, lon: 0, useDebuggerApi: true }
  await settings({ command: 'cover', changes: tokyo })
  const tab = await evaluate('chrome.tabs.create({url:"about:blank",active:false})')
  const deepStatus = await waitFor(async () => {
    const answer = await status(tab.id)
    return ['applied', 'failed'].includes(answer.status.state) && answer.status
  }, 'Deep application result')
  await evaluate(`chrome.tabs.update(${tab.id},{url:${JSON.stringify(url)}})`)
  let observed = await loaded(tab.id)
  if (deepStatus.state === 'applied') {
    for (const key of ['early', 'page', 'blob', 'file', 'module']) {
      assert.equal(observed[key].zone, 'Asia/Tokyo', key)
      assert.equal(observed[key].locale, 'ja-JP', key)
    }
    for (const key of ['same', 'cross']) assert.equal(observed[key].early.zone, 'Asia/Tokyo', key)
    assert.equal((await status(tab.id)).status.state, 'applied')
  } else {
    assert.match(deepStatus.detail, /browser blocks Deep mode/)
    assert.equal((await status(tab.id)).status.state, 'failed')
    console.log('Deep overrides blocked by this browser; explicit failure status verified')
  }
  assert.equal(observed.file.loads, 'ok')
  assert.equal(observed.module.loads, 'ok')
  assert.match(observed.header, /^ja-JP/)

  if (deepStatus.state === 'failed') {
    await waitFor(() => evaluate(`document.querySelector('#tabs').value === '${tab.id}' && !document.querySelector('#recoverDeep').hidden`), 'Deep recovery button')
    await evaluate('document.querySelector("#recoverDeep").click()')
    await waitFor(async () => (await status(tab.id)).status.mode === 'Stealth', 'recovered tab reloaded in Stealth')
    assert.equal(await evaluate("chrome.storage.local.get('sites').then(s=>s.sites.localhost.useDebuggerApi)"), false)
    assert.equal(await evaluate("chrome.storage.local.get('useDebuggerApi').then(s=>s.useDebuggerApi)"), true)
    await settings({ command: 'removeSite', site: 'localhost' })
    await evaluate(`chrome.tabs.reload(${tab.id})`)
    await waitFor(async () => (await status(tab.id)).status.state === 'failed', 'restored Deep mode for transition test')
  }
  const controls = await browser.page('chrome://extensions')
  await waitFor(() => browser.evaluate(controls.sessionId, '!!chrome.developerPrivate'), 'extension controls')
  await browser.evaluate(controls.sessionId, `chrome.developerPrivate.updateExtensionConfiguration({extensionId:'${browser.extensionId}',userScriptsAccess:true})`)
  await settings({ command: 'cover', site: 'localhost', changes: { ...tokyo, useDebuggerApi: false } })
  assert.equal((await status(tab.id)).status.state, 'reload-required')
  await waitFor(async () => (await evaluate('chrome.userScripts.getScripts()')).length, 'document-start script')
  await evaluate(`chrome.tabs.reload(${tab.id})`)
  await waitFor(async () => (await status(tab.id)).status.mode === 'Stealth', 'Stealth application')
  observed = await loaded(tab.id)
  for (const key of ['early', 'page', 'blob']) assert.equal(observed[key].zone, 'Asia/Tokyo', key)
  for (const key of ['same', 'cross']) assert.equal(observed[key].early.zone, 'Asia/Tokyo', key)
  assert.match((await status(tab.id)).status.detail, /document start/)

  await settings({ command: 'cover', site: 'localhost', changes: { timezone: 'Europe/Oslo', locale: 'nb-NO' } })
  await waitFor(async () => (await status(tab.id)).status.state === 'reload-required', 'pending Stealth edit')
  assert.equal(await read(tab.id, 'Intl.DateTimeFormat().resolvedOptions().timeZone'), 'Asia/Tokyo')
  await evaluate(`chrome.tabs.reload(${tab.id})`)
  await waitFor(async () => await read(tab.id, 'Intl.DateTimeFormat().resolvedOptions().timeZone') === 'Europe/Oslo', 'updated Stealth cover')
  await loaded(tab.id)
  assert.deepEqual(await read(tab.id, "[new Date(2024,2,31,2,30).toISOString(),new Date('2024-10-27T02:30').toISOString()]"),
    ['2024-03-31T01:30:00.000Z', '2024-10-27T00:30:00.000Z'], 'missing and repeated local times in Oslo')
  const frameHeader = await evaluate(`chrome.scripting.executeScript({target:{tabId:${tab.id},allFrames:true},func:async()=>({host:location.hostname,lang:await fetch('/headers').then(r=>r.text())})}).then(results=>results.map(r=>r.result))`)
  assert.match(frameHeader.find(r=>r.host === '127.0.0.1').lang, /^nb-NO/, 'cross-site frame sends the top site language')
  const scripts = await evaluate('chrome.userScripts.getScripts()')
  assert.ok(scripts.filter(s=>s.world === 'MAIN').every(s=>!s.js[0].code.includes('localhost')), 'page scripts never receive the rule list')
  // A policy without blob: in connect-src refuses any read of the blob worker's script.
  const strictTab = await evaluate(`chrome.tabs.create({url:${JSON.stringify(`${url}?csp`)},active:false})`)
  const strict = await loaded(strictTab.id)
  assert.deepEqual([strict.blob.zone, strict.blob.language], ['Europe/Oslo', 'nb-NO'], 'blob worker under connect-src')
  await evaluate(`chrome.tabs.remove(${strictTab.id})`)

  const { targetInfos } = await browser.send('Target.getTargets')
  const target = targetInfos.find((target) => target.type === 'page' && target.url === url)
  const { sessionId: webpage } = await browser.send('Target.attachToTarget', { targetId: target.targetId, flatten: true })
  await browser.send('Page.bringToFront', {}, webpage)
  await browser.send('Browser.grantPermissions', { origin: new URL(url).origin, permissions: ['geolocation'] })
  await browser.send('Emulation.setGeolocationOverride', { latitude: 9, longitude: 10, accuracy: 1 }, webpage)
  const position = await read(tab.id, `new Promise((resolve,reject)=>navigator.geolocation.getCurrentPosition(position=>{
    let nativeReadable = false;
    try { Object.getOwnPropertyDescriptor(GeolocationCoordinates.prototype,'latitude').get.call(position.coords); nativeReadable = true } catch {}
    resolve({latitude:position.coords.latitude,longitude:position.coords.longitude,nativeReadable})
  },reject,{timeout:2000}))`)
  assert.deepEqual(position, { latitude: 0, longitude: 0, nativeReadable: false })
  console.log('ok real native geolocation cannot be recovered')
  await read(tab.id, `(()=>{
    const original = fetch;
    window.fetch = (url,options) => url === 'https://httpbingo.org/headers'
      ? Promise.resolve(Response.json({headers:{'Accept-Language':['nb-NO,nb;q=0.9']}})) : original(url,options);
    window.RTCPeerConnection = class { createDataChannel() {} async createOffer(){return {}} async setLocalDescription(){this.onicecandidate({candidate:null})} close(){} };
    return true;
  })()`)
  await waitFor(() => evaluate(`document.querySelector('#tabs').value === '${tab.id}'`), 'diagnostic tab selector')
  await evaluate('document.querySelector("#checkTab").click()')
  await waitFor(() => evaluate('!document.querySelector("#results").hidden'), 'one-click diagnostics')
  assert.equal(await evaluate('document.querySelectorAll("#results tbody tr").length'), 7)
  assert.match(await evaluate('document.querySelector("#results").textContent'), /No candidates; inconclusive/)
  console.log('ok diagnostic UI with local test responses')

  await settings({ command: 'profile', name: 'Work', cover: tokyo })
  await waitFor(() => evaluate('!!document.querySelector("#profiles option[value=Work]")'), 'saved profile')
  await evaluate('document.querySelector("#profiles").value="Work"; document.querySelector("#profiles").dispatchEvent(new Event("change")); document.querySelector("#loadProfile").click()')
  assert.equal(await evaluate('document.querySelector("#timezone").value'), 'Asia/Tokyo')
  assert.equal(await evaluate('document.querySelector("#lat").value'), '0')
  await evaluate('document.querySelector("#editor").requestSubmit()')
  await waitFor(() => evaluate('document.querySelector("#notice").textContent.startsWith("Cover saved")'), 'manager save')
  assert.equal(await evaluate('document.querySelector("#results").hidden'), true, 'settings changes invalidate diagnostics')
  await evaluate('document.querySelector("#checkTab").click()')
  await waitFor(() => evaluate('!document.querySelector("#results").hidden'), 'fresh diagnostic results')
  await evaluate('document.querySelector("#tabs").dispatchEvent(new Event("change"))')
  assert.equal(await evaluate('document.querySelector("#results").hidden'), true, 'tab changes invalidate diagnostics')
  await evaluate('document.querySelector("#checkTab").click(); document.querySelector("#tabs").dispatchEvent(new Event("change"))')
  await waitFor(() => evaluate('document.querySelector("#checkTab").textContent === "Run diagnostics"'), 'canceled diagnostic context')
  assert.equal(await evaluate('document.querySelector("#results").hidden'), true, 'stale asynchronous results stay hidden')
  await evaluate('document.querySelector("#checkTab").click()')
  await waitFor(() => evaluate('!document.querySelector("#results").hidden'), 'current diagnostics')

  const expiry = Date.now() + 3600000
  await settings({ command: 'cover', site: 'temporary.test', changes: { ...tokyo, expiresAt: expiry } })
  await waitFor(() => evaluate(`!!document.querySelector("[data-site='temporary.test']")`), 'temporary site rule')
  await evaluate(`document.querySelector("[data-site='temporary.test']").click(); document.querySelector('#profiles').value='Work'; document.querySelector('#profiles').dispatchEvent(new Event('change')); document.querySelector('#loadProfile').click(); document.querySelector('#editor').requestSubmit()`)
  await waitFor(() => evaluate("document.querySelector('#notice').textContent.startsWith('Cover saved')"), 'profile applied to temporary rule')
  assert.equal(await evaluate("chrome.storage.local.get('sites').then(s=>s.sites['temporary.test'].expiresAt)"), expiry)
  const backup = { version: 1, cover: { ...tokyo, useDebuggerApi: false }, sites: { 'temporary.test': { ...tokyo, locale: 'fr-FR' }, 'added.test': tokyo }, profiles: { Work: { ...tokyo, locale: 'de-DE' } } }
  const previewFile = () => evaluate(`(() => {const input=document.querySelector('#importFile'), files=new DataTransfer(); files.items.add(new File([${JSON.stringify(JSON.stringify(backup))}],'covers.json')); input.files=files.files; input.dispatchEvent(new Event('change'))})()`)
  await previewFile()
  await waitFor(() => evaluate("!document.querySelector('#importPreview').hidden"), 'import preview')
  const preview = await evaluate("document.querySelector('#importChanges').textContent")
  for (const label of ['Replace All sites','Replace Site temporary.test','Add Site added.test','Replace Profile Work']) assert.ok(preview.includes(label), label)
  assert.equal(await evaluate("chrome.storage.local.get('sites').then(s=>!!s.sites['added.test'])"), false)
  await evaluate("document.querySelector('#cancelImport').click()")
  await previewFile()
  await waitFor(() => evaluate("!document.querySelector('#importPreview').hidden"), 'second preview')
  await evaluate("document.querySelector('#confirmImport').click()")
  await waitFor(() => evaluate("chrome.storage.local.get('sites').then(s=>!!s.sites['added.test'])"), 'import applied')
  await evaluate("document.querySelector('#undoImport').click()")
  await waitFor(() => evaluate("chrome.storage.local.get('sites').then(s=>!s.sites['added.test'])"), 'import undone')
  assert.equal(await evaluate("chrome.storage.local.get('sites').then(s=>s.sites['temporary.test'].expiresAt)"), expiry)
  console.log('ok profile expiry, import preview, cancel, apply and undo')

  await settings({ command: 'cover', changes: { sealWebRtc: true } })
  await settings({ command: 'cover', site: 'localhost', changes: { useDebuggerApi: false, sealWebRtc: true } })
  const effectivePolicy = 'chrome.privacy.network.webRTCIPHandlingPolicy.get({}).then(s=>s.value)'
  await waitFor(async () => await evaluate(effectivePolicy) === 'disable_non_proxied_udp', 'global WebRTC seal')
  await evaluate(`chrome.tabs.update(${tab.id},{active:true})`)
  const { sessionId: policyPopup } = await browser.page(`chrome-extension://${browser.extensionId}/html/popup.html`, true)
  const popupRead = expression => browser.evaluate(policyPopup, expression)
  await waitFor(() => popupRead("document.querySelector('#webRtcNote')?.textContent.includes('Sealed for all sites')"), 'effective seal in popup')
  const { sessionId: controller } = await browser.privacyController()
  await waitFor(() => browser.evaluate(controller, "!!globalThis.chrome?.privacy"), 'test controller ready')
  await browser.evaluate(controller, "chrome.privacy.network.webRTCIPHandlingPolicy.set({value:'default'})")
  await waitFor(() => popupRead("document.querySelector('#webRtcNote')?.textContent.includes('Site relay requested')"), 'site fallback after loss of browser policy')
  await popupRead("document.querySelector('#scopeAll').click()")
  await waitFor(() => popupRead("document.querySelector('#webRtcNote')?.textContent.includes('blocked by another extension') && document.querySelector('[value=open]').checked"), 'blocked browser seal is displayed as open')
  assert.ok(await popupRead(`(() => {
    const box = selector => document.querySelector(selector).getBoundingClientRect()
    return document.body.scrollWidth === document.body.clientWidth &&
      box('.tick-boxes').right <= box('#webRtcRow').right &&
      box('#webRtcNote').top >= box('.tick-boxes').bottom
  })()`), 'WebRTC status and controls stay within the popup width')
  const documentId = () => evaluate(`chrome.webNavigation.getFrame({ tabId: ${tab.id}, frameId: 0 }).then(f => f?.documentId)`)
  const previousDocument = await documentId()
  await evaluate(`chrome.tabs.reload(${tab.id})`)
  await waitFor(async () => { const id = await documentId(); return id && id !== previousDocument }, 'reloaded policy test document')
  await loaded(tab.id)
  assert.equal(await read(tab.id, "(()=>{const pc=new RTCPeerConnection();try{return pc.getConfiguration().iceTransportPolicy}finally{pc.close()}})()"), 'relay')
  await browser.evaluate(controller, "chrome.privacy.network.webRTCIPHandlingPolicy.clear({})")
  await waitFor(async () => await evaluate(effectivePolicy) === 'disable_non_proxied_udp', 'browser seal restored')
  await settings({ command: 'cover', changes: { sealWebRtc: false } })
  await settings({ command: 'cover', site: 'localhost', changes: { sealWebRtc: false } })
  console.log('ok real Chrome WebRTC policy conflict, site relay and recovery')

  await evaluate(`(() => {
    const send = chrome.runtime.sendMessage.bind(chrome.runtime)
    chrome.runtime.sendMessage = (message, ...args) => message.lookupIp
      ? Promise.resolve({ ok: true, cover: { timezone: 'Europe/Oslo', locale: 'nb-NO', lat: 59.9, lon: 10.7 } })
      : send(message, ...args)
    document.querySelector('[data-site=""]').click()
    const preset = document.querySelector('#preset')
    preset.value = 'ipAddress'
    preset.dispatchEvent(new Event('input', { bubbles: true }))
    preset.dispatchEvent(new Event('change'))
  })()`)
  await waitFor(() => evaluate("document.querySelector('#timezone').value === 'Europe/Oslo' && !document.querySelector('#preset').disabled"), 'IP location preview')
  await evaluate("document.querySelector('#notice').hidden = true; document.querySelector('#editor').requestSubmit()")
  await waitFor(() => evaluate("!document.querySelector('#notice').hidden && document.querySelector('#notice').textContent.startsWith('Cover saved')"), 'IP-following cover saved')
  assert.equal(await evaluate("chrome.storage.local.get('configuration').then(s => s.configuration)"), 'ipAddress')
  assert.equal(await evaluate("document.querySelector('#preset').value"), 'ipAddress')
  const { sessionId: reopenedManager } = await browser.page(`chrome-extension://${browser.extensionId}/html/manage.html`, true)
  await waitFor(() => browser.evaluate(reopenedManager, "document.querySelector('#preset')?.value === 'ipAddress'"), 'saved IP source after reopening Manage')
  console.log('ok manager keeps the IP location source after saving and reopening')

  if (process.env.CAPTURE_DIR) {
    await mkdir(process.env.CAPTURE_DIR, { recursive: true })
    const capture = async (session, name, width, height, scheme) => {
      await browser.send('Page.bringToFront', {}, session)
      await browser.send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: false }, session)
      await browser.send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: scheme }, { name: 'prefers-reduced-motion', value: 'reduce' }] }, session)
      await browser.evaluate(session, 'document.fonts.ready')
      await browser.evaluate(session, 'window.scrollTo(0,0)')
      await browser.evaluate(session, 'new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))')
      const { cssContentSize } = await browser.send('Page.getLayoutMetrics', {}, session)
      assert.ok(cssContentSize.width <= width, `${name} overflows horizontally`)
      const { data } = await browser.send('Page.captureScreenshot', { captureBeyondViewport: true, clip: { x: 0, y: 0, width, height: Math.max(height, cssContentSize.height), scale: 1 } }, session)
      await writeFile(join(process.env.CAPTURE_DIR, `${name}.png`), Buffer.from(data, 'base64'))
    }
    await capture(manager, 'desktop', 1280, 900, 'light')
    await capture(manager, 'mobile', 390, 844, 'dark')
    await evaluate(`chrome.tabs.update(${tab.id},{active:true})`)
    const { sessionId: popup } = await browser.page(`chrome-extension://${browser.extensionId}/html/popup.html`, true)
    await waitFor(() => browser.evaluate(popup, 'document.querySelector("#configuration")?.value === "custom"'), 'popup settings')
    await browser.send('Page.bringToFront', {}, popup)
    await waitFor(() => browser.evaluate(popup, 'document.body.classList.contains("settled")'), 'popup')
    await capture(popup, 'popup', 400, 600, 'dark')
    const height = await browser.evaluate(popup, 'document.documentElement.scrollHeight')
    assert.ok(height <= 600, `Popup height ${height} exceeds 600`)
  }
  console.log('ok real Chrome: Deep capability/failure, frames/workers/header, Stealth document-start/update, manager/profile/zero coordinates')
} finally {
  await browser?.close()
  server.closeAllConnections()
  await new Promise((resolve) => server.close(resolve))
}
