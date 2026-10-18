// Serves the Alibi check page: yarn testpage
// Open http://localhost:8642/ — its cross-site iframe loads from 127.0.0.1, a different site.
// Add ?delay=0 or ?delay=150 to control how long the HTML takes to arrive, ?csp for a strict policy.
import http from 'node:http'
import { readFile } from 'node:fs/promises'

const port = Number(process.env.PORT) || 8642
const root = new URL('..', import.meta.url)
const shared = { '/': 'test/check.html', '/css/style.css': 'css/style.css', '/fonts/archivo.woff2': 'fonts/archivo.woff2', '/mark.svg': 'img/mark.svg' }
const types = { html: 'text/html; charset=utf-8', js: 'text/javascript', mjs: 'text/javascript', json: 'application/json', css: 'text/css', woff2: 'font/woff2', svg: 'image/svg+xml' }

http
  .createServer(async (req, res) => {
    const url = new URL(req.url, 'http://x')
    // Anything else is a file straight from test/ (frame.html, the workers and their helpers).
    const file = shared[url.pathname] ?? (/^\/[\w-]+\.\w+$/.test(url.pathname) ? `test${url.pathname}` : null)
    let body = file && (await readFile(new URL(file, root)).catch(() => null))
    if (!body) return res.writeHead(404).end('Not found')
    // Echo the request's Accept-Language so the page can compare it with what scripts see.
    if (url.pathname === '/') body = String(body).replace('%ACCEPT_LANGUAGE%', (req.headers['accept-language'] ?? '').replace(/[&"<>]/g, ''))
    const headers = { 'content-type': types[file.split('.').pop()], 'cache-control': 'no-store' }
    // ?csp blocks reading blob: URLs, ?csp=strict blocks blob workers outright: Alibi must fall back cleanly
    const csp = url.searchParams.get('csp')
    if (csp !== null) headers['content-security-policy'] = csp === 'strict' ? "worker-src 'self'; connect-src 'self'" : "connect-src 'self'; worker-src 'self' blob:"
    res.writeHead(200, headers)
    res.flushHeaders() // headers now, body after the delay, like a real network response
    const delay = Math.min(Number(url.searchParams.get('delay')) || 0, 5000)
    setTimeout(() => res.end(body), url.pathname === '/' ? delay : 0)
  })
  .listen(port, () => {
    console.log(`Alibi check page:  http://localhost:${port}/`)
    console.log(`Same page, other site:  http://127.0.0.1:${port}/`)
  })
