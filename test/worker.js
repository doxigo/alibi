// A worker loaded from a file. Like real workers, it loads files relative to itself, so the check
// page can tell whether Alibi's wrapping broke anything.
const report = (loads) =>
  postMessage({
    zone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    offset: -new Date().getTimezoneOffset(),
    locale: Intl.NumberFormat().resolvedOptions().locale,
    loads,
  })
try {
  importScripts('worker-helper.js') // sets self.helper
  fetch('worker-data.json')
    .then((response) => response.json())
    .then((data) => report(self.helper && data.ok && location.pathname.endsWith('/worker.js') ? 'ok' : `location reads ${location.pathname}`))
    .catch((error) => report(error.message))
} catch (error) {
  report(error.message)
}
