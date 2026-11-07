// Runs in the selected page, so its CSP and Alibi cover both apply.
export default async function diagnose() {
  const read = () => ({ timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, locale: Intl.DateTimeFormat().resolvedOptions().locale, language: navigator.language })
  const result = { page: read() }
  const attempt = async (job) => {
    try { return await job() } catch (error) { return { error: error.message } }
  }
  result.frame = await attempt(() => {
    const frame = document.createElement('iframe')
    frame.hidden = true
    document.documentElement.append(frame)
    try {
      const realm = frame.contentWindow
      return { timezone: realm.Intl.DateTimeFormat().resolvedOptions().timeZone, locale: realm.Intl.DateTimeFormat().resolvedOptions().locale, language: realm.navigator.language }
    } finally { frame.remove() }
  })
  const worker = async (type) => {
    const url = URL.createObjectURL(new Blob([`postMessage((${read})())`], { type: 'text/javascript' }))
    let instance, timer
    try {
      return await new Promise((resolve, reject) => {
        instance = new Worker(url, { type })
        instance.onmessage = ({ data }) => resolve(data)
        instance.onerror = () => reject(new Error('Worker blocked or failed; this surface was not verified.'))
        timer = setTimeout(() => reject(new Error('Worker timed out.')), 4000)
      })
    } finally {
      clearTimeout(timer)
      instance?.terminate()
      URL.revokeObjectURL(url)
    }
  }
  const rtc = async () => {
    let pc, timer
    const addresses = new Set()
    try {
      pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] })
      pc.createDataChannel('alibi-check')
      const done = new Promise((resolve) => {
        pc.onicecandidate = ({ candidate }) => {
          if (!candidate) return resolve()
          if (candidate.address) addresses.add(candidate.address)
        }
        timer = setTimeout(resolve, 4000)
      })
      await pc.setLocalDescription(await pc.createOffer())
      await done
      return { addresses: [...addresses], note: 'No candidates is inconclusive; a failed STUN request can produce the same result.' }
    } finally { clearTimeout(timer); pc?.close() }
  }
  const [classic, module, headers, webrtc, geolocation] = await Promise.all([
    attempt(() => worker('classic')),
    attempt(() => worker('module')),
    attempt(async () => {
      const response = await fetch('https://httpbingo.org/headers', { credentials: 'omit', referrerPolicy: 'no-referrer', cache: 'no-store', signal: AbortSignal.timeout(6000), redirect: 'error' })
      if (!response.ok) throw new Error(`Header check returned ${response.status}.`)
      const { headers } = await response.json()
      const entry = Object.entries(headers ?? {}).find(([name]) => name.toLowerCase() === 'accept-language')
      if (!entry) throw new Error('The server did not report Accept-Language.')
      return { language: [entry[1]].flat().join(',') }
    }),
    attempt(rtc),
    attempt(async () => {
      const permission = await navigator.permissions.query({ name: 'geolocation' })
      if (permission.state !== 'granted') return { error: 'Location permission is not granted; no permission prompt was opened.' }
      return await new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('Location check timed out. The site may need to be in the foreground.')), 4000)
        navigator.geolocation.getCurrentPosition(
          ({ coords }) => { clearTimeout(timer); resolve({ latitude: coords.latitude, longitude: coords.longitude }) },
          (error) => { clearTimeout(timer); reject(new Error(error.message)) }, { timeout: 4000, maximumAge: 0 })
      })
    }),
  ])
  return { ...result, classic, module, headers, webrtc, geolocation }
}
