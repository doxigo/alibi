// Injected into every frame's MAIN world by background.js when Deep mode is off.
// Must stay self-contained: chrome.scripting.executeScript serializes this function, and it
// re-serializes itself (by its own name) into the blob workers pages start.
// ponytail: workers started from the site's own files keep the real zone. Wrapping them means a blob,
// and a page whose policy forbids blob workers fails that worker silently (no error to fall back on).
// Third-party scripts, where fingerprinting comes from, can't start file workers anyway: a worker's
// script must come from the page's own site, so they use blob workers, which are covered.
export default function stealth(config) {
  // Cloudflare Turnstile's frame fails the check (and the site's sign-in) when its navigator.language(s)
  // differ from its own blob worker's. That frame's policy blocks reading the worker's script, so the
  // worker can't be covered and already shows Cloudflare the real browser. Patching the frame would hide
  // nothing, so it is left alone. `origin` also matches its about:blank and blob: children.
  if (globalThis.origin === 'https://challenges.cloudflare.com') return
  let { timezone, locale, lat, lon } = config
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
  // "Match my IP address" while the Mac's own zone is already the exit's (Whereabouts moves it there):
  // leave Date native, so the next VPN switch reaches this page live, the way Chrome passes on any
  // system time zone change. A fixed cover keeps its zone even if the Mac's moves later.
  // Zones compare as the browser names them: a system it calls Asia/Calcutta is on Asia/Kolkata.
  if (config.configuration === 'ipAddress' && timezone &&
    new Intl.DateTimeFormat('en-US', { timeZone: timezone }).resolvedOptions().timeZone === Intl.DateTimeFormat().resolvedOptions().timeZone) timezone = null
  if (!(timezone || locale || lat || lon)) return // e.g. a site set to its real location
  const cover = { timezone, locale, lat, lon }
  // The cover's locale wherever the browser would use its own: no locales at all, or an empty list,
  // as in toLocaleTimeString([], { hour: '2-digit' }). Anything else passes through, errors and all.
  const orCover = (l) => {
    if (!locale) return l
    if (l === undefined) return locale
    try {
      return typeof l === 'object' && l !== null && Intl.getCanonicalLocales(l).length === 0 ? locale : l
    } catch {
      return l
    }
  }

  // A Proxy alone prints as "function () { [native code] }" (no name), so map each one back to
  // the function it wraps when Function.prototype.toString is asked. Shared by every realm we patch.
  const natives = new WeakMap()
  const disguise = (proxy, orig) => (natives.set(proxy, orig), proxy)
  const hook = (obj, name, apply) => {
    obj[name] = disguise(new Proxy(obj[name], { apply }), obj[name])
  }
  const hookGetter = (proto, name, after) => {
    const desc = Object.getOwnPropertyDescriptor(proto, name)
    if (!desc?.get) return
    const get = disguise(new Proxy(desc.get, { apply: (f, self, args) => after(Reflect.apply(f, self, args)) }), desc.get)
    Object.defineProperty(proto, name, { ...desc, get })
  }

  // Patch one JavaScript world: this page, a fresh iframe or popup window, or a worker.
  const patch = (realm) => {
    const { Intl, Date, Object } = realm
    hook(realm.Function.prototype, 'toString', (f, fn, args) => Reflect.apply(f, natives.get(fn) ?? fn, args))

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
        wrapCtor(name, ([l, ...rest]) => [orCover(l), ...rest])
      }
      hook(realm.Number.prototype, 'toLocaleString', (f, n, [l, o]) => Reflect.apply(f, n, [orCover(l), o]))
      hook(realm.BigInt.prototype, 'toLocaleString', (f, n, [l, o]) => Reflect.apply(f, n, [orCover(l), o]))
      hook(realm.String.prototype, 'localeCompare', (f, s, [that, l, o]) => Reflect.apply(f, s, [that, orCover(l), o]))
    }

    if (locale) {
      // Same list as the Accept-Language header background.js sets: 'ja-JP' -> ['ja-JP', 'ja'].
      const languages = realm.Object.freeze(realm.Array.from(new Set([locale, locale.split('-')[0]])))
      for (const proto of [realm.Navigator?.prototype, realm.WorkerNavigator?.prototype]) {
        if (!proto) continue
        hookGetter(proto, 'language', () => locale)
        hookGetter(proto, 'languages', () => languages)
      }
    }

    if (timezone || locale) {
      const withDefaults = ([l, o]) => [
        orCover(l),
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
      realm.Date = FakeDate
    }

    if ((lat || lon) && realm.Geolocation) {
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
        hook(realm.Geolocation.prototype, name, (f, geo, [success, ...rest]) =>
          Reflect.apply(f, geo, [typeof success === 'function' ? (pos) => success(spoofPosition(pos)) : success, ...rest])
        )
      }
    }

    // A same-origin iframe or popup starts as a fresh world with untouched Intl and Date. Patch it
    // the moment the page first reaches it. Cross-origin ones throw here and get their own injection.
    // ponytail: frames[n] / window[n] reach a fresh world without these getters; hook DOM insertion if that matters.
    const adopt = (child) => {
      try {
        if (child && child !== realm && !natives.has(child.Function.prototype.toString)) patch(child)
      } catch {}
      return child
    }
    if (realm.HTMLIFrameElement) {
      hookGetter(realm.HTMLIFrameElement.prototype, 'contentWindow', adopt)
      hookGetter(realm.HTMLIFrameElement.prototype, 'contentDocument', (doc) => (adopt(doc?.defaultView), doc))
    }
    if (typeof realm.open === 'function') hook(realm, 'open', (f, self, args) => adopt(Reflect.apply(f, self, args)))

    // Blob workers start from a copy of their script with this same function in front. The script is
    // read now because pages often revoke the URL right away, and blob workers resolve nothing
    // relative to themselves, so the move changes nothing. One URL always maps to one copy, so a
    // SharedWorker started twice in a page is still one worker.
    // ponytail: a module worker's own imports evaluate before its body, so they run unpatched.
    const copies = new Map()
    for (const name of ['Worker', 'SharedWorker']) {
      const Orig = realm[name]
      if (!Orig) continue
      realm[name] = disguise(new Proxy(Orig, {
        construct: (target, args, newTarget) => {
          const url = String(args[0])
          // Two kinds of site would refuse the copy below and report the attempt to the site, so their
          // worker starts as it is: one on Trusted Types (Google Meet), which hands over a TrustedScriptURL
          // and refuses a plain URL in its place, and one whose Permissions-Policy turns off synchronous
          // requests (Cloudflare's dashboard), which refuses the read.
          // ponytail: those workers keep the real zone in Stealth; a Trusted Types policy of Alibi's own could
          // cover the first kind where the site allows one. Deep covers both already.
          const refused = realm.trustedTypes?.isScriptURL(args[0]) || realm.document?.featurePolicy?.allowsFeature('sync-xhr') === false
          if (!url.startsWith('blob:') || refused) return Reflect.construct(target, args, newTarget)
          try {
            let copy = copies.get(url)
            if (!copy) {
              const xhr = new realm.XMLHttpRequest()
              xhr.open('GET', url, false)
              xhr.send()
              const source = xhr.responseText
              const strict = source.match(/^\s*(['"])use strict\1;?/)?.[0] ?? '' // must stay first
              const boot = `${strict}\n(${stealth})(${JSON.stringify(cover)});\n${source.slice(strict.length)}`
              copy = realm.URL.createObjectURL(new realm.Blob([boot], { type: 'text/javascript' }))
              copies.set(url, copy)
            }
            return Reflect.construct(target, [copy, ...args.slice(1)], newTarget)
          } catch {
            return Reflect.construct(target, args, newTarget) // e.g. a policy that blocks reading blob URLs
          }
        },
      }), Orig)
    }
  }

  patch(globalThis)
}
