// A module worker loaded from a file, importing and fetching relative to itself.
import { helper } from './worker-helper.mjs'

let loads
try {
  const data = await (await fetch('worker-data.json')).json()
  loads = helper && data.ok && location.pathname.endsWith('/worker.mjs') ? 'ok' : `location reads ${location.pathname}`
} catch (error) {
  loads = error.message
}
postMessage({
  zone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  offset: -new Date().getTimezoneOffset(),
  locale: Intl.NumberFormat().resolvedOptions().locale,
  loads,
})
