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
    value = await read()
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
