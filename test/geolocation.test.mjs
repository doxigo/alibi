import assert from 'node:assert/strict'
class NativeCoordinates {
  #latitude = 9
  get latitude() { return this.#latitude }
  get longitude() { return 10 }
  toJSON() { return { latitude: this.#latitude, longitude: 10 } }
}
globalThis.Geolocation = class {
  getCurrentPosition(success) { success({ timestamp: 1, coords: new NativeCoordinates() }) }
  watchPosition(success) { this.getCurrentPosition(success); return 7 }
}
const revoked = []
const originalRevoke = URL.revokeObjectURL.bind(URL)
URL.revokeObjectURL = (url) => { revoked.push(url); originalRevoke(url) }
globalThis.XMLHttpRequest = class { open() {} send() { this.responseText = 'postMessage(1)' } }
globalThis.Worker = class { constructor(url) { this.url = url } }
const { default: stealth } = await import('../js/stealth.js')
const config = { lat: 0, lon: 0, documentStart: true }
assert.equal(stealth(config).early, true)
const patched = Geolocation.prototype.getCurrentPosition
stealth(config)
assert.equal(Geolocation.prototype.getCurrentPosition, patched, 'commit fallback must not wrap a second time')
new Geolocation().getCurrentPosition((position) => {
  assert.equal(position.coords.latitude, 0)
  assert.throws(() => Object.getOwnPropertyDescriptor(NativeCoordinates.prototype, 'latitude').get.call(position.coords))
  assert.throws(() => NativeCoordinates.prototype.toJSON.call(position.coords))
  delete position.coords.latitude
  assert.notEqual(position.coords.latitude, 9)
})
const worker = new Worker('blob:https://a.test/one')
assert.notEqual(worker.url, 'blob:https://a.test/one')
URL.revokeObjectURL('blob:https://a.test/one')
assert.deepEqual(revoked, [worker.url, 'blob:https://a.test/one'])
assert.notEqual(new Worker('blob:https://a.test/one').url, worker.url)
console.log('ok native geolocation getters cannot recover coordinates; worker copies are revoked')
