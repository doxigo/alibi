// Serves the Alibi check page: node test/serve.mjs  (or yarn testpage)
// Open http://localhost:8642/ — its cross-site iframe loads from 127.0.0.1, a different site.
// Add ?delay=0 or ?delay=150 to control how long the HTML takes to arrive.
import http from 'node:http'
import { readFile } from 'node:fs/promises'

const port = Number(process.env.PORT) || 8642
const root = new URL('..', import.meta.url)
const files = {
  '/': ['test/check.html', 'text/html; charset=utf-8'],
  '/frame.html': ['test/frame.html', 'text/html; charset=utf-8'],
  '/css/style.css': ['css/style.css', 'text/css'],
  '/fonts/archivo.woff2': ['fonts/archivo.woff2', 'font/woff2'],
  '/mark.svg': ['img/mark.svg', 'image/svg+xml'],
}

http
  .createServer(async (req, res) => {
    const url = new URL(req.url, 'http://x')
    const file = files[url.pathname]
    if (!file) return res.writeHead(404).end('Not found')
    const body = await readFile(new URL(file[0], root))
    res.writeHead(200, { 'content-type': file[1], 'cache-control': 'no-store' })
    res.flushHeaders() // headers now, body after the delay, like a real network response
    const delay = Math.min(Number(url.searchParams.get('delay')) || 0, 5000)
    setTimeout(() => res.end(body), url.pathname === '/' ? delay : 0)
  })
  .listen(port, () => {
    console.log(`Alibi check page:  http://localhost:${port}/`)
    console.log(`Same page, other site:  http://127.0.0.1:${port}/`)
  })
