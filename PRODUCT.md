# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Primary: privacy-minded people browsing through a VPN or proxy who want their browser's time zone, locale and geolocation to agree with their exit IP, so sites cannot tell where they actually are. Secondary: developers and QA testers checking how a site behaves for visitors in another country, time zone or locale.

## Product Purpose

Alibi is a Chromium extension (Chrome, Brave) that makes the browser report a different time zone, locale and geolocation. Success: you pick a place once, and every page you load tells the same consistent story.

## Positioning

Two modes with an honest trade-off. Stealth Mode (default) patches the page's JavaScript APIs and never attaches the debugger, so Chrome shows no "started debugging this browser" banner. When "Allow User Scripts" is on, it runs before any page script. Deep Mode uses `chrome.debugger` CDP emulation for browser-level spoofing, and Chrome shows the banner. Alibi forks Cloaq; the banner-free Stealth Mode is the difference.

## Operating Context

- Used mostly from the toolbar popup (fixed 400px wide), in short bursts: choose a preset location, "Match IP Address", or custom values, then reload the page.
- The info page opens on install as onboarding and is also the options page.
- Settings: configuration preset, time zone (IANA), locale (BCP 47), latitude and longitude, and the Deep Mode toggle. "Match my IP address" fetches get.geojs.io over HTTPS, only when chosen.
- Runs in Brave and Chrome, often alongside a VPN extension or client.

## Capabilities and Constraints

- Manifest V3; vanilla JS ES modules, no framework. Tailwind CSS 3 builds `tailwind.css` into `css/style.css`. Static HTML pages live in `html/`.
- Stealth Mode covers new iframes and blob Web Workers, not workers started from the site's own files (wrapping those breaks sites that forbid blob workers). Deep Mode covers those too, but shows Chrome's banner.
- The locale also drives `navigator.language(s)` and the `Accept-Language` header (declarativeNetRequest rules, per site).
- Alibi doesn't change the IP address; you need a VPN or proxy for that.
- The Chrome Web Store restricts the `userScripts` API. A store build may need to drop it.
- `_locales` holds the extension name and description in 51 languages. The UI copy itself is English-only.

## Brand Commitments

- Name: Alibi. It replaces Cloaq's name, icon, logo and colors completely, and nothing visual carries over from Cloaq.
- License: GPL-3.0. Cloaq must be credited as the upstream project.
- To be published publicly (Chrome Web Store and/or a public GitHub repo), so the icon has to read at 16px in a toolbar and look right among store listings.

## Evidence on Hand

- No testimonials, user counts, store ratings or press exist. Don't invent any.
- Verified behavior from end-to-end tests in Chrome for Testing 155: Stealth spoofs time zone, locale, geolocation and iframes without attaching the debugger. With User Scripts allowed, it catches inline `<head>` scripts.

## Product Principles

1. Honest about trade-offs: always say which mode is active and what it can't cover.
2. One consistent story: time zone, locale and coordinates should agree with each other and with the chosen place.
3. Quiet by default: no banners, no nagging. Alibi stays out of the way until you need it.
4. Fast to change: switching location takes one or two clicks from the popup.
