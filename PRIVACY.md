# Alibi privacy policy

Alibi changes what websites see about your time zone, language and location. It does this inside your browser, and it doesn't collect, sell or share anything about you.

## What Alibi stores

Your settings stay in your browser's extension storage (`chrome.storage.local`) and are never sent anywhere:

- the place you picked, time zone, locale, latitude and longitude
- per-site covers, keyed by site name (for example `example.com`)
- Stealth or Deep mode
- whether a companion app answers, and the time zone it holds your computer's clock on, for the popup to show

Uninstalling Alibi deletes them.

## The one network request

Alibi contacts a server only when you choose **Match my IP address**, and then once a minute for as long as any cover stays on that option, so the cover follows your VPN when its exit changes. Each time it asks [GeoJS](https://www.geojs.io/) (`https://get.geojs.io/v1/ip/geo.json`) where your connection appears to come from. Like any server, GeoJS sees your IP address when it answers. Alibi uses the answer (time zone, country, coordinates) only to fill in your settings, and stores it in the same local storage. GeoJS's own policy covers what it does with requests.

If [Whereabouts](https://github.com/doxigo/whereabouts), the companion Mac app, is running, Alibi asks it instead of GeoJS, over Chrome's native messaging, which stays on your Mac. Whereabouts tells Alibi the time zone, locale and coordinates of your VPN exit each time the exit changes. Alibi tells it whether any cover is on Match my IP address. The next Whereabouts ([doxigo/whereabouts#1](https://github.com/doxigo/whereabouts/pull/1)) can also keep the Mac's clock on Alibi's location, and Alibi then sends it one more thing over the same channel: the configuration, time zone and locale of your All sites cover, whenever you change it. Earlier versions get no cover. The Chrome Web Store build doesn't do this.

On Windows and Linux, [Alibi Companion](README.md#alibi-companion-windows-and-linux) plays the same part if you install it. It passes the time zone, the country's locale and the coordinates of your connection to Alibi over native messaging, which stays on your computer, and Alibi tells it whether any cover is on Match my IP address. On Windows, Alibi also sends it the configuration, time zone and locale of your All sites cover, whenever you change it, so that it can set Windows' clock and region to a place you choose there, if you turn that on. On Linux, Alibi sends it nothing else.

- On Linux, your browser starts it while a cover is on Match my IP address, and for a moment when the browser starts, when Alibi is installed, and, while a place is set for all sites and Alibi hasn't found a companion that takes it, when you choose it or open the popup, to see that it's there. It asks the same GeoJS address only while a cover is on Match my IP address: every 60 seconds, and a few more times whenever your network changes, so GeoJS sees your IP address that often. It stores nothing and contacts no other server.
- On Windows, its tray app runs from when you start it (from the Start menu, or at every sign-in if you turn that on) until you quit it, whether or not a browser is open, and asks GeoJS on that schedule all along. If you turn on Keep Windows on my VPN's time zone, it sets Windows' time zone, and when the zone's name depends on it Windows' Country or region, to the connection's while a VPN is up, or, if you turned that on too, to a place you chose in Alibi for all sites. Whenever the connection moves, and every 5 minutes, it also checks where DNS and IPv6 come out. It looks up `whoami.akamai.net` through your computer's DNS, so Akamai's servers see which DNS resolver asked (not your address), and asks GeoJS where that resolver is, once an hour at most for the same one. Then it asks GeoJS once over IPv6, if you have it. In the companion's folder it keeps your own time zone and region and what it last set Windows to (`home.json`, so it can put them back), your tray settings (`settings.json`), each open browser's All sites cover (`covers\`), its latest answer (`agent.json`) and a log of what it changed (`agent.log`). These stay on your computer, and uninstalling deletes them.

With no cover on Match my IP address, opening the popup, changing places and browsing make no requests of Alibi's own.

## What Alibi doesn't do

- No analytics, tracking, ads or accounts.
- No reading or storing of page content, history or form data.
- No selling or transferring of data, and no use of data for anything other than spoofing the settings you chose.

## Permissions, and why

- **Read and change data on all websites, scripting:** so the spoofed time zone, language and location reach every page and frame.
- **declarativeNetRequest:** so the `Accept-Language` header matches the language you picked.
- **debugger:** only for Deep mode, which you turn on yourself. Chrome then shows a "started debugging this browser" banner.
- **webNavigation:** to apply the right cover as pages load.
- **storage:** for your settings.
- **alarms:** to re-check your IP address once a minute while a cover is on Match my IP address.
- **nativeMessaging:** to hear from Whereabouts (macOS) or Alibi Companion (Windows, Linux) about a new VPN exit while a cover is on Match my IP address, to tell them whether one is, and to tell a companion that can keep the system's clock on Alibi's location (Alibi Companion on Windows, Whereabouts from doxigo/whereabouts#1) your All sites cover's time zone and locale. Not in the Chrome Web Store build.

## Contact

Alibi is open source (GPL-3.0). Ask questions or report problems in the project's GitHub issues.
