# Alibi privacy policy

Alibi changes what websites see about your time zone, language and location. It does this inside your browser, and it doesn't collect, sell or share anything about you.

## What Alibi stores

Your settings stay in your browser's extension storage (`chrome.storage.local`) and are never sent anywhere:

- the place you picked, time zone, locale, latitude and longitude
- per-site covers, keyed by site name (for example `example.com`)
- Stealth or Deep mode

Uninstalling Alibi deletes them.

## The one network request

Alibi contacts a server only when you choose **Match my IP address**, and then once a minute for as long as any cover stays on that option, so the cover follows your VPN when its exit changes. Each time it asks [GeoJS](https://www.geojs.io/) (`https://get.geojs.io/v1/ip/geo.json`) where your connection appears to come from. Like any server, GeoJS sees your IP address when it answers. Alibi uses the answer (time zone, country, coordinates) only to fill in your settings, and stores it in the same local storage. GeoJS's own policy covers what it does with requests.

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

## Contact

Alibi is open source (GPL-3.0). Ask questions or report problems in the project's GitHub issues.
