import assert from 'node:assert/strict'
const calls = []
let fail = false
globalThis.chrome = {
  runtime: { getManifest: () => ({ permissions: [] }) },
  debugger: {
    attach: async ({ tabId }) => calls.push(['attach', tabId]),
    detach: async ({ tabId }) => calls.push(['detach', tabId]),
    sendCommand: async (target, method, params) => {
      calls.push([method, target.tabId, params])
      if (fail && method === 'Emulation.setTimezoneOverride' && params.timezoneId) throw new Error('override refused')
    },
  },
}
const { applyDebugger, forgetDebugger, startChild } = await import('../js/debugger.js')
const cover = { timezone: 'Asia/Tokyo', locale: 'ja-JP', lat: 0, lon: 0 }
await applyDebugger(1, cover, true)
assert.deepEqual(calls.find(([method]) => method === 'Emulation.setGeolocationOverride')[2], { latitude: 0, longitude: 0, accuracy: 1 })
const appliedCalls = calls.length
await applyDebugger(1, cover, true)
assert.equal(calls.length, appliedCalls)
fail = true
await assert.rejects(applyDebugger(2, cover), /override refused/)
fail = false
await applyDebugger(2, cover)
assert.equal(calls.filter(([method, id, params]) => method === 'Emulation.setTimezoneOverride' && id === 2 && params.timezoneId).length, 2)
await applyDebugger(1, {})
assert.deepEqual(calls.filter(([method]) => method === 'detach'), [['detach', 1]])
await startChild({ tabId: 2 }, { sessionId: 'worker', waitingForDebugger: true, targetInfo: { type: 'worker' } }, async () => cover)
assert.equal(calls.at(-1)[0], 'Runtime.runIfWaitingForDebugger')
fail = true
await assert.rejects(startChild({ tabId: 2 }, { sessionId: 'failed-worker', waitingForDebugger: true, targetInfo: { type: 'worker' } }, async () => cover), /override refused/)
assert.equal(calls.at(-1)[0], 'Runtime.runIfWaitingForDebugger', 'failed child must still resume')
fail = false
const beforeRetry = calls.length
await applyDebugger(2, cover)
assert.ok(calls.length > beforeRetry, 'child failure must invalidate the applied cache')
forgetDebugger(2)
await applyDebugger(2, cover)
assert.equal(calls.filter(([method, id]) => method === 'attach' && id === 2).length, 2)
console.log('ok debugger errors, retry, zero coordinates, child resume and per-tab ownership')
