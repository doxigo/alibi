import { spawn } from 'node:child_process'
import { cp, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { once } from 'node:events'

export async function browserSession() {
  const dir = await mkdtemp(join(tmpdir(), 'alibi-browser-'))
  const extension = join(dir, 'extension')
  for (const path of ['manifest.json', '_locales', 'html', 'js', 'css', 'img', 'fonts']) {
    await cp(new URL(`../${path}`, import.meta.url), join(extension, path), { recursive: true })
  }
  const manifest = JSON.parse(await readFile(join(extension, 'manifest.json'), 'utf8'))
  manifest.permissions = manifest.permissions.filter((permission) => permission !== 'nativeMessaging')
  await writeFile(join(extension, 'manifest.json'), JSON.stringify(manifest))
  const executable = process.env.CHROME_PATH ?? (process.platform === 'darwin'
    ? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' : 'google-chrome')
  const browser = spawn(executable, ['--headless=new', `--user-data-dir=${join(dir, 'profile')}`,
    '--remote-debugging-pipe', '--enable-unsafe-extension-debugging', '--no-first-run', '--no-default-browser-check',
    '--disable-background-networking', '--disable-component-update', '--disable-sync', '--disable-default-apps',
    '--metrics-recording-only', '--no-proxy-server', 'about:blank'], { stdio: ['ignore', 'ignore', 'pipe', 'pipe', 'pipe'] })
  let id = 0, buffer = '', stderr = ''
  const pending = new Map()
  browser.stderr.on('data', (chunk) => { stderr = (stderr + chunk).slice(0, 16000) })
  const fail = (error) => { for (const job of pending.values()) job.reject(error); pending.clear() }
  browser.on('error', fail)
  browser.on('exit', (code) => fail(new Error(`Chrome exited (${code}): ${stderr}`)))
  for (const pipe of [browser.stdio[3], browser.stdio[4]]) {
    pipe.on('error', (error) => fail(new Error(`Chrome protocol pipe failed: ${error.message}\n${stderr}`)))
  }
  browser.stdio[4].on('data', (chunk) => {
    buffer += chunk
    let end
    while ((end = buffer.indexOf('\0')) !== -1) {
      const message = JSON.parse(buffer.slice(0, end))
      buffer = buffer.slice(end + 1)
      const job = pending.get(message.id)
      if (!job) continue
      pending.delete(message.id)
      clearTimeout(job.timer)
      message.error ? job.reject(new Error(message.error.message)) : job.resolve(message.result)
    }
  })
  const send = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
    const number = ++id
    const timer = setTimeout(() => { pending.delete(number); reject(new Error(`Timed out: ${method}`)) }, 15000)
    pending.set(number, { resolve, reject: (error) => { clearTimeout(timer); reject(error) }, timer })
    browser.stdio[3].write(`${JSON.stringify({ id: number, method, params, sessionId })}\0`)
  })
  const evaluate = async (session, expression) => {
    const result = await send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true }, session)
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description ?? result.exceptionDetails.text)
    return result.result.value
  }
  const page = async (url, background = false) => {
    const { targetId } = await send('Target.createTarget', { url, background })
    const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true })
    return { targetId, sessionId }
  }
  const close = async () => {
    if (browser.exitCode === null && browser.signalCode === null && !browser.killed) {
      const ended = once(browser, 'exit')
      browser.kill('SIGTERM')
      await ended
    }
    await rm(dir, { recursive: true, force: true })
  }
  try {
    const version = await send('Browser.getVersion')
    const { id: extensionId } = await send('Extensions.loadUnpacked', { path: extension })
    return { send, evaluate, page, close, extensionId, version: version.product }
  } catch (error) { await close(); throw error }
}
