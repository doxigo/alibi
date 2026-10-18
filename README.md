# Alibi

[![Build](https://github.com/doxigo/alibi/actions/workflows/build.yml/badge.svg)](https://github.com/doxigo/alibi/actions/workflows/build.yml)

Check what sites see: [doxigo.github.io/alibi](https://doxigo.github.io/alibi/). Every push builds the unpacked extension as a downloadable artifact (Actions → latest run → `alibi-extension`).

Alibi is a Chrome extension that spoofs your time zone, geolocation, and locale to any location you choose. The locale also sets `navigator.language` and the `Accept-Language` header, so every place a site can ask tells the same story. Use it to test websites in different regions or to match your location data to your VPN's IP address.

Alibi is a fork of [Cloaq](https://github.com/www1z4rd/cloaq) by www1z4rd and is distributed under the same GPL-3.0 license. Changes from Cloaq: it was renamed, and it adds a Stealth Mode that never attaches the debugger, so Chrome's "started debugging this browser" banner doesn't appear.

## Modes

**Stealth Mode (default).** On every navigation, Alibi injects a script into each frame's page context. The script patches `Intl`, `Date`, `toLocaleString` and `navigator.geolocation`. No debugger is involved, so Chrome shows no banner. Limits:

- New iframes and blob Web Workers get the cover too. Workers started from the site's own files still see the real time zone. Covering them means restarting them from a blob, and on sites whose security policy forbids blob workers that fails silently, breaking the site. Third-party scripts, where fingerprinting usually comes from, can't start file workers (a worker's script must come from the page's own site), so they're limited to blob workers, which are covered. Deep covers file workers too.
- An empty iframe reached through `window.frames[n]` before anything touches its `contentWindow` is still unpatched.
- Determined fingerprinting scripts can detect the patches.

For Stealth Mode to also catch scripts that run the instant a page loads, open `chrome://extensions`, click Details on Alibi, and turn on **Allow User Scripts**. Alibi then registers its patches through `chrome.userScripts`, and Chrome runs them before any page script. With the toggle off, Alibi injects on page commit instead, and inline `<head>` scripts can still see real values when the HTML arrives within a few milliseconds of the headers. The popup shows a hint while the toggle is off.

**Deep Mode.** Turn this on in the popup. Alibi attaches `chrome.debugger` and uses the CDP `Emulation.*` overrides. Spoofing then happens below the page, which is much harder to detect, and cross-site iframes and Web Workers get the cover before their first script runs. Leaving the New Tab page (which the debugger can't touch), Alibi briefly parks the tab on a blank page to attach first, then continues to the site, so even that first page is covered from its first script. One limit remains: when you move between two sites with *different* covers in the same tab and the new page's HTML arrives almost instantly, its first inline scripts can see the previous site's cover (never your real location). Chrome holds debugger commands until the navigation commits, and re-sending the navigation could break form submissions. While it's active, Chrome shows the "started debugging this browser" banner. To use Deep Mode without the banner, launch Chrome with `--silent-debugger-extension-api` ([how to pass flags](https://www.chromium.org/developers/how-tos/run-chromium-with-flags/)).

## Per-site covers

The popup has two tabs. **All sites** holds your usual cover. The second tab is named after the current site: choose **Give it its own cover** and that site, along with its subdomains, gets separate settings. The most specific rule wins, and iframes follow the page they're embedded in. To keep one site on your real location, give it its own cover and set it to **Off (real location)**. Stealth/Deep mode applies to all sites.

## IP Address

Alibi does not change your IP address. Use a VPN or proxy for that.

## Permission Justification

**scripting, host permissions (all sites):** Stealth Mode needs these to inject its spoofing script into pages.

**declarativeNetRequestWithHostAccess:** Sets the `Accept-Language` request header to match each site's cover.

**userScripts:** Lets Stealth Mode run before page scripts once you turn on "Allow User Scripts". Note: the Chrome Web Store limits this API to user-script managers, so a store build may need to drop it and rely on the injection fallback.

**debugger:** Deep Mode needs this to attach to tabs and emulate location, time zone, and locale.

**webNavigation:** Detects new page loads so Alibi can apply the active mode.

**storage:** Stores settings and preferences.

## Development

Alibi is built using JavaScript and Tailwind CSS.

```
yarn
yarn build        # outputs to /build
yarn build:store  # Chrome Web Store zip: same build without the userScripts permission
yarn test         # checks stealth time zone math against real TZ processes
yarn testpage     # the check page at http://localhost:8642/, with a second site and delay/policy options
```

### Load the Extension in Chrome

1. Open `chrome://extensions/`.
2. Enable Developer mode.
3. Click "Load unpacked".
4. Select the `build` folder, or the root folder if you're running `yarn watch:css`.
