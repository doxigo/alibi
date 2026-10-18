// Injected into every frame's MAIN world by background.js when Deep mode is off.
// Must stay self-contained: chrome.scripting.executeScript serializes this function.
// ponytail: page-level patches miss Web Workers and pristine about:blank iframe realms; Deep mode covers both.
export default function stealth({ timezone, locale, lat, lon }) {
  // A bad zone or locale would make every Intl call on the page throw; drop it instead.
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: timezone ?? undefined })
  } catch {
    timezone = null
  }
  try {
    Intl.getCanonicalLocales(locale ?? [])
  } catch {
    locale = null
  }
  if (!(timezone || locale || lat || lon)) return // e.g. a site set to its real location

  // A Proxy alone prints as "function () { [native code] }" (no name), so map each one
  // back to the function it wraps when Function.prototype.toString is asked.
  const natives = new WeakMap()
  const disguise = (proxy, orig) => (natives.set(proxy, orig), proxy)
  const hook = (obj, name, apply) => {
    obj[name] = disguise(new Proxy(obj[name], { apply }), obj[name])
  }
  hook(Function.prototype, 'toString', (f, fn, args) => Reflect.apply(f, natives.get(fn) ?? fn, args))

  // Swap an Intl constructor for a Proxy that fills in default arguments.
  const wrapCtor = (name, fill) => {
    const Orig = Intl[name]
    if (!Orig) return
    const proxy = disguise(new Proxy(Orig, {
      construct: (target, args, newTarget) => Reflect.construct(target, fill(args), newTarget),
      // DateTimeFormat / NumberFormat / Collator are also callable without `new`
      apply: (target, _, args) => Reflect.construct(target, fill(args)),
    }), Orig)
    Intl[name] = proxy
    Object.defineProperty(Orig.prototype, 'constructor', { value: proxy, writable: true, configurable: true })
  }

  if (locale) {
    for (const name of ['Collator', 'NumberFormat', 'PluralRules', 'RelativeTimeFormat', 'ListFormat', 'DisplayNames', 'DurationFormat', 'Segmenter']) {
      wrapCtor(name, ([l, ...rest]) => [l ?? locale, ...rest])
    }
    hook(Number.prototype, 'toLocaleString', (f, n, [l, o]) => Reflect.apply(f, n, [l ?? locale, o]))
    hook(BigInt.prototype, 'toLocaleString', (f, n, [l, o]) => Reflect.apply(f, n, [l ?? locale, o]))
    hook(String.prototype, 'localeCompare', (f, s, [that, l, o]) => Reflect.apply(f, s, [that, l ?? locale, o]))
  }

  if (timezone || locale) {
    const withDefaults = ([l, o]) => [
      l ?? locale ?? undefined,
      { ...o, timeZone: o?.timeZone ?? timezone ?? undefined },
    ]
    wrapCtor('DateTimeFormat', withDefaults)
    for (const name of ['toLocaleString', 'toLocaleDateString', 'toLocaleTimeString']) {
      hook(Date.prototype, name, (f, d, args) => Reflect.apply(f, d, withDefaults(args)))
    }
  }

  if (timezone) {
    const OrigDate = Date
    const DP = Date.prototype
    const getTime = DP.getTime
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone: timezone, hourCycle: 'h23', era: 'short',
      year: 'numeric', month: 'numeric', day: 'numeric', hour: 'numeric', minute: 'numeric', second: 'numeric',
    })
    const longName = new Intl.DateTimeFormat(undefined, { timeZone: timezone, timeZoneName: 'long' })

    // Minutes to add to target-zone wall time to get UTC (Date#getTimezoneOffset convention).
    let lastT, lastOffset // pages read several fields of one date in a row
    const offsetAt = (t) => {
      if (Number.isNaN(t)) return NaN
      if (t === lastT) return lastOffset
      const p = {}
      for (const { type, value } of parts.formatToParts(t)) p[type] = value
      const wall = new OrigDate(0)
      wall.setUTCFullYear(p.era === 'BC' ? 1 - p.year : +p.year, p.month - 1, p.day)
      wall.setUTCHours(p.hour, p.minute, p.second, 0)
      const wholeSecond = t - ((t % 1000) + 1000) % 1000
      lastT = t
      return (lastOffset = Math.round((wholeSecond - getTime.call(wall)) / 60000))
    }
    // UTC ms <-> "local" ms (target-zone wall clock stored as if it were UTC).
    const toLocal = (t) => t - offsetAt(t) * 60000
    const fromLocal = (l) => l + offsetAt(l + offsetAt(l) * 60000) * 60000 // second pass settles DST edges
    const shifted = (d) => new OrigDate(toLocal(getTime.call(d)))
    const realOffset = DP.getTimezoneOffset
    const realParse = OrigDate.parse

    // Local-time getters read the UTC fields of the shifted instant.
    for (const name of ['FullYear', 'Month', 'Date', 'Day', 'Hours', 'Minutes', 'Seconds', 'Milliseconds']) {
      const utcGetter = DP[`getUTC${name}`]
      hook(DP, `get${name}`, (_, d) => utcGetter.call(shifted(d)))
    }
    hook(DP, 'getYear', (_, d) => DP.getUTCFullYear.call(shifted(d)) - 1900)
    hook(DP, 'getTimezoneOffset', (_, d) => offsetAt(getTime.call(d)))

    // Local-time setters: edit the shifted copy with the UTC setter, then map back.
    for (const name of ['FullYear', 'Month', 'Date', 'Hours', 'Minutes', 'Seconds', 'Milliseconds']) {
      const utcSetter = DP[`setUTC${name}`]
      hook(DP, `set${name}`, (_, d, args) => {
        const s = shifted(d)
        // per spec, setFullYear on an invalid date starts from local +0
        if (name === 'FullYear' && Number.isNaN(getTime.call(s))) s.setTime(0)
        Reflect.apply(utcSetter, s, args)
        return DP.setTime.call(d, fromLocal(getTime.call(s)))
      })
    }

    const pad = (n, w = 2) => String(n).padStart(w, '0')
    const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
    const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
    const dateString = (d) => {
      const s = shifted(d)
      const y = s.getUTCFullYear()
      return `${DAYS[s.getUTCDay()]} ${MONTHS[s.getUTCMonth()]} ${pad(s.getUTCDate())} ${y < 0 ? '-' : ''}${pad(Math.abs(y), 4)}`
    }
    const timeString = (d) => {
      const s = shifted(d)
      const off = -offsetAt(getTime.call(d))
      const name = longName.formatToParts(d).find((p) => p.type === 'timeZoneName')?.value
      const gmt = `GMT${off < 0 ? '-' : '+'}${pad(Math.floor(Math.abs(off) / 60))}${pad(Math.abs(off) % 60)}`
      return `${pad(s.getUTCHours())}:${pad(s.getUTCMinutes())}:${pad(s.getUTCSeconds())} ${gmt} (${name})`
    }
    const valid = (d) => !Number.isNaN(getTime.call(d))
    hook(DP, 'toDateString', (f, d) => (valid(d) ? dateString(d) : Reflect.apply(f, d, [])))
    hook(DP, 'toTimeString', (f, d) => (valid(d) ? timeString(d) : Reflect.apply(f, d, [])))
    hook(DP, 'toString', (f, d) => (valid(d) ? `${dateString(d)} ${timeString(d)}` : Reflect.apply(f, d, [])))

    // Strings without an explicit zone were parsed as real-local time; re-anchor them to the target zone.
    // ponytail: regex heuristic; misreads oddities like "2024-1-10" (V8 parses that as local, regex says zoned).
    const zoned = /(?:z|[+-]\d\d(?::?\d\d)?|\b(?:ut|utc|gmt|[ecmp][sd]t))\s*(?:\(.*\))?\s*$/i
    const dateOnlyISO = /^[+-]?\d{4,6}(?:-\d\d(?:-\d\d)?)?$/
    const parse = (str) => {
      const t = realParse(str)
      str = String(str).trim()
      if (Number.isNaN(t) || zoned.test(str) || dateOnlyISO.test(str)) return t
      return fromLocal(t - Reflect.apply(realOffset, new OrigDate(t), []) * 60000)
    }
    hook(OrigDate, 'parse', (_, __, [str]) => parse(str))

    const FakeDate = disguise(new Proxy(OrigDate, {
      apply: () => new FakeDate().toString(),
      construct: (target, args, newTarget) => {
        if (args.length === 1 && typeof args[0] === 'string') args = [parse(args[0])]
        else if (args.length >= 2) args = [fromLocal(OrigDate.UTC(...args))]
        return Reflect.construct(target, args, newTarget)
      },
    }), OrigDate)
    Object.defineProperty(DP, 'constructor', { value: FakeDate, writable: true, configurable: true })
    globalThis.Date = FakeDate
  }

  if ((lat || lon) && globalThis.Geolocation) {
    const coords = {
      latitude: +lat || 0, longitude: +lon || 0, accuracy: 20,
      altitude: null, altitudeAccuracy: null, heading: null, speed: null,
    }
    // The real request still runs, so the permission prompt behaves normally;
    // the page only ever sees the spoofed coordinates.
    const spoofPosition = (pos) => {
      const define = (obj, key, desc) => Object.defineProperty(obj, key, { ...desc, configurable: true })
      for (const k in coords) define(pos.coords, k, { get: () => coords[k] })
      define(pos.coords, 'toJSON', { value: () => ({ ...coords }) })
      define(pos, 'toJSON', { value: () => ({ timestamp: pos.timestamp, coords: { ...coords } }) })
      return pos
    }
    for (const name of ['getCurrentPosition', 'watchPosition']) {
      hook(Geolocation.prototype, name, (f, geo, [success, ...rest]) =>
        Reflect.apply(f, geo, [typeof success === 'function' ? (pos) => success(spoofPosition(pos)) : success, ...rest])
      )
    }
  }
}
