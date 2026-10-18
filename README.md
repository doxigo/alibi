# Alibi

Alibi is a Chrome extension that spoofs your time zone, geolocation, and locale to any location you choose. Use it to test websites in different regions or to match your location data to your VPN's IP address.

Alibi is a fork of [Cloaq](https://github.com/www1z4rd/cloaq) by www1z4rd and is distributed under the same GPL-3.0 license. Changes from Cloaq: it was renamed, and it adds a Stealth Mode that never attaches the debugger, so Chrome's "started debugging this browser" banner doesn't appear.

## Modes

**Stealth Mode (default).** On every navigation, Alibi injects a script into each frame's page context. The script patches `Intl`, `Date`, `toLocaleString` and `navigator.geolocation`. No debugger is involved, so Chrome shows no banner. Limits:

- Web Workers and freshly created `about:blank` iframes still see the real time zone.
- Determined fingerprinting scripts can detect the patches.

For Stealth Mode to also catch scripts that run the instant a page loads, open `chrome://extensions`, click Details on Alibi, and turn on **Allow User Scripts**. Alibi then registers its patches through `chrome.userScripts`, and Chrome runs them before any page script. With the toggle off, Alibi injects on page commit instead, and inline `<head>` scripts can still see real values when the HTML arrives within a few milliseconds of the headers. The popup shows a hint while the toggle is off.

**Deep Mode.** Turn this on in the popup. Alibi attaches `chrome.debugger` and uses the CDP `Emulation.*` overrides. Spoofing then happens below the page, which is much harder to detect, and cross-site iframes and Web Workers get the cover before their first script runs. Limits: on the first page you open from a New Tab page, Deep can only attach once that page has started loading, so its cross-site iframes see your real location (every later page in the tab is covered). And when a page's HTML arrives almost instantly, its first inline scripts can see the previous value: your real location on a new tab's first page, or the previous site's cover when you move between sites with different covers. While it's active, Chrome shows the "started debugging this browser" banner. To use Deep Mode without the banner, launch Chrome with `--silent-debugger-extension-api` ([how to pass flags](https://www.chromium.org/developers/how-tos/run-chromium-with-flags/)).

## Per-site covers

The popup has two tabs. **All sites** holds your usual cover. The second tab is named after the current site: choose **Give it its own cover** and that site, along with its subdomains, gets separate settings. The most specific rule wins, and iframes follow the page they're embedded in. To keep one site on your real location, give it its own cover and set it to **Off (real location)**. Stealth/Deep mode applies to all sites.

## IP Address

Alibi does not change your IP address. Use a VPN or proxy for that.

## Permission Justification

**scripting, host permissions (all sites):** Stealth Mode needs these to inject its spoofing script into pages.

**userScripts:** Lets Stealth Mode run before page scripts once you turn on "Allow User Scripts". Note: the Chrome Web Store limits this API to user-script managers, so a store build may need to drop it and rely on the injection fallback.

**debugger:** Deep Mode needs this to attach to tabs and emulate location, time zone, and locale.

**webNavigation:** Detects new page loads so Alibi can apply the active mode.

**storage:** Stores settings and preferences.

## Development

Alibi is built using JavaScript and Tailwind CSS.

```
yarn
yarn build   # outputs to /build
yarn test    # checks stealth time zone math against real TZ processes
```

### Load the Extension in Chrome

1. Open `chrome://extensions/`.
2. Enable Developer mode.
3. Click "Load unpacked".
4. Select the `build` folder, or the root folder if you're running `yarn watch:css`.
