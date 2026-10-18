// Differential check: Date/Intl in a process spoofed to ZONE must match a process really running in ZONE.
// Run: node test/stealth.test.mjs
import { execFileSync } from 'node:child_process'
import assert from 'node:assert/strict'

const PROBE = `
const out = []
const stamps = [1600000000000, 1711846800000, 1730595600000, 1704067200123, Date.UTC(2024, 2, 31, 0, 59)]
for (const t of stamps) {
  const d = new Date(t)
  out.push(d.toString(), d.getTimezoneOffset(), d.getHours(), d.getDate(), d.getDay(), d.toLocaleString('en-US'))
}
out.push(Intl.DateTimeFormat().resolvedOptions().timeZone)
out.push(new Date(2024, 2, 31, 2, 30).getTime(), new Date(2024, 9, 27, 12).getTime(), new Date(99, 0).getTime())
out.push(new Date('2024-06-01T10:00').getTime(), new Date('2024-06-01').getTime(), Date.parse('Jun 1 2024 10:00'))
out.push(new Date('2024-06-01T10:00Z').getTime(), Date.parse('Sat, 01 Jun 2024 10:00:00 GMT'))
const s = new Date(1717236000000); s.setHours(23, 15); s.setDate(31); out.push(s.getTime())
const y = new Date(NaN); y.setFullYear(2020); out.push(y.getTime())
out.push(String(new Date(NaN)), typeof Date(), new Date(5) instanceof Date, Date.prototype.constructor === Date)
console.log(JSON.stringify(out))
`
const run = (tz, spoof) =>
  JSON.parse(execFileSync('node', ['--no-warnings', '--input-type=module', '-e',
    spoof
      ? `import spoof from ${JSON.stringify(new URL('../js/stealth.js', import.meta.url).href)}; spoof({ timezone: ${JSON.stringify(spoof)} });${PROBE}`
      : PROBE,
  ], { env: { ...process.env, TZ: tz, LANG: 'en_US.UTF-8' } }).toString())

for (const zone of ['Europe/Berlin', 'Asia/Kolkata', 'America/Sao_Paulo', 'Australia/Lord_Howe']) {
  assert.deepEqual(run('America/New_York', zone), run(zone), zone)
  console.log('ok', zone)
}

// Locale defaults + geolocation shape, in one spoofed process.
execFileSync('node', ['--no-warnings', '--input-type=module', '-e', `
  import assert from 'node:assert/strict'
  globalThis.Geolocation = class { getCurrentPosition(ok) { ok({ timestamp: 1, coords: { latitude: 9, longitude: 9 } }) } watchPosition() {} }
  import(${JSON.stringify(new URL('../js/stealth.js', import.meta.url).href)}).then(({ default: s }) => {
    s({ locale: 'de-DE', lat: 52.52, lon: 13.4 })
    assert.equal((1234.5).toLocaleString(), '1.234,5')
    assert.equal(new Intl.NumberFormat().resolvedOptions().locale, 'de-DE')
    assert.equal(Intl.DateTimeFormat().resolvedOptions().locale, 'de-DE')
    assert.equal(navigator.language, 'de-DE')
    assert.deepEqual([...navigator.languages], ['de-DE', 'de'])
    assert.equal(new Date(0).toLocaleDateString(undefined, { timeZone: 'UTC' }), '1.1.1970')
    assert.equal(Function.prototype.toString.call(Intl.NumberFormat), 'function NumberFormat() { [native code] }')
    assert.equal(String(Number.prototype.toLocaleString), 'function toLocaleString() { [native code] }')
    assert.equal(Function.prototype.toString.call(Function.prototype.toString), 'function toString() { [native code] }')
    new Geolocation().getCurrentPosition((p) => {
      assert.equal(p.coords.latitude, 52.52)
      assert.deepEqual(JSON.parse(JSON.stringify(p)).coords.longitude, 13.4)
    })
  })
`], { stdio: 'inherit' })
console.log('ok locale + geolocation')
