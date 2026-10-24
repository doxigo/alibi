# Alibi

![Alibi: your browser was somewhere else](store/marquee.png)

[![Build](https://github.com/doxigo/alibi/actions/workflows/build.yml/badge.svg)](https://github.com/doxigo/alibi/actions/workflows/build.yml)

Check what sites see: [doxigo.github.io/alibi](https://doxigo.github.io/alibi/). Download: [latest release](https://github.com/doxigo/alibi/releases/latest).

Alibi is a Chrome extension that spoofs your time zone, geolocation, and locale to any location you choose. The locale also sets `navigator.language` and the `Accept-Language` header, so every place a site can ask tells the same story. Use it to test websites in different regions or to match your location data to your VPN's IP address.

Alibi is a fork of [Cloaq](https://github.com/www1z4rd/cloaq) by www1z4rd and is distributed under the same GPL-3.0 license. Changes from Cloaq: it was renamed, and it adds a Stealth Mode that never attaches the debugger, so Chrome's "started debugging this browser" banner doesn't appear.

## Install

1. Download `alibi-vX.Y.Z.zip` from the [latest release](https://github.com/doxigo/alibi/releases/latest) and unzip it into a folder you'll keep.
2. Open `chrome://extensions` (in Brave, `brave://extensions`) and turn on **Developer mode**.
3. Click **Load unpacked** and pick that folder.
4. Optional, recommended: click **Details** on Alibi and turn on **Allow User Scripts**, so Stealth Mode runs before any page script.

To update, unzip the new release over the same folder and click the reload icon on Alibi's card.

The steps are the same on Windows, macOS and Linux. To have **Match my IP address** follow your VPN the moment it changes exits, add [Whereabouts](#back-it-up-with-whereabouts) on a Mac or [Alibi Companion](#alibi-companion-windows-and-linux) on Windows and Linux. Without them, Alibi checks once a minute (see [IP Address](#ip-address)).

Alibi's ID is the same wherever you load it from (`dhlpjjenplighmpbogjpmljbahbnjpfa`, pinned by the `key` in `manifest.json`), so Whereabouts and Alibi Companion can tell it apart from other extensions. Versions before the pinned ID got an ID from their folder. If you're updating from one of those, reloading isn't enough: remove the old card, then click **Load unpacked** again. Chrome sees a new extension, so your covers start fresh.

## Screenshots

| Every site | One site, its own cover | Deep mode, dark theme |
|---|---|---|
| <img src="store/src/popup-all-light.png" alt="Popup: sites see you in Tokyo, with the All sites tab selected" width="260"> | <img src="store/src/popup-site-light.png" alt="Popup: bookshop.example sees you in Paris, on that site's own tab" width="260"> | <img src="store/src/popup-deep-dark.png" alt="Popup in dark mode with Deep mode on and Reload to apply highlighted" width="260"> |

The postmark at the top shows what the current site sees: its city, local time and UTC offset.

## Back it up with Whereabouts

<img src="docs/whereabouts-icon.png" alt="Whereabouts app icon: a passport entry stamp reading Whereabouts, Admitted" width="112" align="right">

**Alibi gives your browser a story. [Whereabouts](https://github.com/doxigo/whereabouts) makes your whole Mac stick to it.**

A browser extension can only argue from inside the browser. A site's own workers, and the first scripts of a page that loads instantly, read the time zone straight from macOS, and macOS still knows where you really are. Whereabouts is a free, open source menu bar app that closes that gap from the other side. It asks one question all day long: *where does the internet think this Mac is right now?*

- **Your location shows as a place, not a status.** A flag, a country code or a city sits in the menu bar, never just "VPN: connected".
- **When it's wrong, you'll know.** Choose any mix of a red menu bar, a notification, a sound and a red line along the edge of the screen. An optional kill switch cuts the network until you're back where you meant to be.
- **The Mac's clock moves to your exit.** While the VPN is up, macOS switches to the exit's time zone, so every browser, worker and app agrees. The menu bar keeps your real time: `Oslo · 14:32 home`.
- **It catches the quiet leaks.** IPv6 exits, DNS resolvers, an unexpected ISP, and a Mac whose time zone or region tells a different story than its connection.
- **It works with rules about places.** Allow or block countries, expect certain ISPs, or give a site its own location, matched against the browser tab you're on.

| Where a site can look | Alibi | Whereabouts |
|---|:---:|:---:|
| Page scripts, iframes, blob workers: time zone, locale, language, location | ✓ | |
| The `Accept-Language` header | ✓ | |
| The site's own workers and the first scripts of instant pages: time zone | | ✓ |
| Your public IP and VPN exit, with alerts and a kill switch | | ✓ |
| IPv6 and DNS leaks | | ✓ |

Run both, with Alibi on **Match my IP address** so both follow your VPN exit (Whereabouts tells Alibi the moment the exit changes), and every time zone on the [check page](https://doxigo.github.io/alibi/) agrees, including the ones Alibi can't reach. (A site's own workers still read the browser's locale; Alibi's Deep mode covers that.)

The next Whereabouts ([doxigo/whereabouts#1](https://github.com/doxigo/whereabouts/pull/1), after 1.2.0) can also keep the Mac on Alibi's location, not only the VPN's. With **Match my Mac's time zone and region** and **Follow Alibi's location** both on, a place you choose for **All sites** in Alibi moves the Mac to its time zone and region. **Off** and **Match my IP address** leave the Mac to follow your connection, as without Alibi. Follow Alibi's location is off unless you turn it on, because it moves the whole Mac, not just the browser.

Whereabouts runs on macOS only. On Windows, [Alibi Companion](#alibi-companion-windows-and-linux) is the same kind of app, in the tray: your location as a place, alerts when it's wrong, Windows' clock on your VPN's time zone (or on Alibi's location, if you like), and IPv6 and DNS checks. On Linux it tells Alibi the moment your VPN exit changes.

## Alibi Companion (Windows and Linux)

Alibi Companion does for Windows what Whereabouts does for a Mac, and on Linux the part that Alibi talks to.

**On Windows** it's a tray app that starts when you sign in:

- **Your location shows as a place.** The tray icon is the code of the country your connection comes out in, on Alibi's blue stamp. Hover over it for the city and the time at home. Click it for the rest: the network you come out of, your IP address, and whether Windows' time zone, IPv6 and DNS all tell the same story.
- **When something's wrong, you'll know.** The stamp turns red, and you get a notification, a sound and a red line along the top of the screen, each of which you can switch off in the menu. That happens when your connection comes out at home (the VPN dropped), in a country you blocked, or outside the ones you allow, and when IPv6 or DNS goes around the VPN.
- **Windows' clock follows your VPN.** With **Keep Windows on my VPN's time zone** on, Windows moves to the time zone of wherever your connection comes out, and back to your own when it comes out at home. Every browser, worker and app then agrees with your IP. The companion changes Windows' **Country or region** only when the zone's name needs it: Norway's zone reads as `Europe/Oslo` only with the region on Norway, and as `Europe/Berlin` otherwise.
- **And, if you like, Alibi's location.** Under it, **or on Alibi's location for all sites**, off unless you turn it on, lets a place you choose for **All sites** in Alibi move Windows there instead, within a second: a city or a custom zone. **Off** and **Match my IP address** leave Windows on your connection's zone. A per-site cover stays in the browser, since Windows has one clock. If Windows' clock and your connection are in different countries (Alibi on Tokyo, the VPN in Norway), the tray says so, because sites can compare the two.
- **Your own time zone stays yours.** A time zone or region you set yourself, or that Windows' own automatic time zone sets, becomes home from then on; while the VPN is up, its zone goes back on. When you switch following off, quit the app, sign out, shut down or uninstall, Windows goes back home, once, unless something else has moved it since, and is then left alone.
- **It tells Alibi the moment your exit changes**, so **Match my IP address** follows within seconds instead of within a minute.

Windows' own **Set time zone automatically** works against all this: it moves the zone to where Windows' location service puts the computer, which is your real location. The companion never changes that setting itself. The installer and the tray menu open Settings > Time & language > Date & time, where you switch it off, and uninstalling reminds you that you can switch it back on there.

You can choose which countries count. `settings.json` in the companion's folder takes `homeCountry` (by default, the country of the time zone Windows had when the companion first ran), `allowedCountries` and `blockedCountries`, for example `{"allowedCountries": ["NO", "SE"]}`. After editing it, quit the app from the tray and run the installer again, or sign out and back in.

**On Linux** it tells Alibi the moment your exit changes, and that's all: there's no tray, and the system's time zone stays as it is.

It's made for a VPN that covers the whole computer. It asks where your connection comes out the same way the rest of the computer connects, so with a proxy set only in your browser it would report your real location. If you proxy just the browser, skip the companion: Alibi's own once-a-minute check goes through the browser's proxy and gets it right.

**Install**

1. From the [latest release](https://github.com/doxigo/alibi/releases/latest), download the companion for your system: `alibi-companion-windows-x64.exe` (`-arm64.exe` for Windows on Arm), or `alibi-companion-linux-x64` (`-arm64`).
2. **Windows:** double-click it. It isn't code-signed, so SmartScreen may stop it first: choose **More info**, then **Run anyway**. The tray app starts right away. Windows 11 puts new tray icons behind the **^** arrow; to keep the stamp in view, drag it onto the taskbar, or turn it on under Settings > Personalization > Taskbar > Other system tray icons. To leave Windows' time zone alone, run `alibi-companion-windows-x64.exe install --no-timezone` from a terminal instead; you can turn following on later from the tray. **Linux:** run `chmod +x alibi-companion-linux-x64 && ./alibi-companion-linux-x64 install`.
3. In Alibi, choose **Match my IP address**. A browser that's already open finds the companion within a minute.

Install copies the program into your user folder (`%LOCALAPPDATA%\Programs\Alibi Companion` on Windows, `~/.local/share/alibi-companion` on Linux), so you can delete the download afterwards. It then registers it, for your user only and without admin rights, as the native messaging host `com.doxigo.whereabouts`, which only Alibi may start. On Windows that's a key under `HKEY_CURRENT_USER` for each of Chrome, Edge, Brave and Chromium, an **Alibi Companion** entry among the programs Windows starts at sign-in, and an entry in Settings > Apps > Installed apps. On Linux it's a file in the profile folder of each Chrome, Chromium, Brave, Edge or Vivaldi it finds, so open a newly installed browser once before you install. It won't replace a registration that belongs to another program unless you run `install --force`. To update, run the new download the same way: it keeps your settings. A browser that's running the old copy keeps it until it next starts the companion.

- `alibi-companion status` shows where it's registered, whether the tray app runs, how Windows' time zone and region are set and what browsers read from them, and where your connection appears to be.
- `alibi-companion agent --dry-run` (Windows) shows what the time zone sync would set right now, and changes nothing.
- `alibi-companion uninstall`, or **Uninstall** on Alibi Companion in Settings > Apps > Installed apps, stops the app, puts Windows' time zone and region back home if they're still where it left them, and removes the program, its registrations and its settings. `--quiet` asks nothing.

Every 2 seconds it compares your network adapters. When one comes up, goes down or changes address, it asks [GeoJS](https://www.geojs.io/) where the connection now comes out, a few times over the next 20 seconds while the new routes settle. It also asks every 30 seconds, for exits that change without the adapters changing. On Windows the tray app does the asking, from sign-in on, and the part the browser starts passes on its answer. That answer names the zone the way Windows now reads it (`America/New_York` for a Detroit exit, as on any real Windows PC there), so Alibi recognises the system as already matching. Without the tray app (on Linux, or after you quit it), the part the browser starts asks GeoJS itself, and only while a cover is on **Match my IP address**: picking a city asks nothing.

Alibi and the companion talk over Chrome's native messaging, on your computer only. Alibi says first whether any cover is on Match my IP address, `{"following": true}`, and again whenever that changes. The companion says hello, and on Windows that it takes covers: `{"hello": "alibi-companion", "accepts": ["cover"]}`. Alibi then sends it the **All sites** cover's configuration, time zone and locale whenever they change, `{"cover": {"configuration": "tokyo", "timezone": "Asia/Tokyo", "locale": "ja-JP"}}`, and keeps the port open while that cover is a place. The companion answers with places, as Whereabouts does, and with the zone it holds Windows on, `{"system": "Asia/Tokyo"}`, or `{"system": null}` while it holds none, which the popup shows. Pages keep their own cover either way. Whereabouts speaks the same protocol on the Mac from [doxigo/whereabouts#1](https://github.com/doxigo/whereabouts/pull/1) on (`"hello": "whereabouts"`). A companion that says no hello, like Whereabouts 1.2.0 and earlier, gets no cover, as before. On Windows each part the browser starts keeps its browser's cover for the tray app in a file of its own, `covers\cover-<process id>.json`, and deletes it as it exits; the tray app skips any whose browser part is gone.

Whenever the connection moves, and every 5 minutes, the tray app also checks where DNS and IPv6 come out. For DNS it looks up `whoami.akamai.net`, whose servers answer with the address of the DNS resolver that asked them, and asks GeoJS where that address is. For IPv6 it asks GeoJS once more, over IPv6 only. It keeps `home.json` (your own time zone and region, and what it last set Windows to), `settings.json`, `covers\` (each open browser's cover), `agent.json` (its latest answer, for the browser) and `agent.log` in the companion's folder.

Limits:

- It has no kill switch. Use your VPN's own (WireGuard: **Block untunneled traffic**), which stops traffic before any of it can leave the tunnel.
- While it follows your VPN, a zone you set by hand becomes home, and the VPN's zone goes back on. Turn off **Keep Windows on my VPN's time zone** in the tray menu to keep your own.
- Windows has one clock, so it follows the **All sites** cover only. A site with its own cover gets it in the browser only, as before. With Alibi in more than one browser or profile, Windows follows the newest place among those still open; Off or Match my IP address in one never outvotes a place in another.
- Windows has one clock for everyone signed in, too: one user's companion keeps it at a time. Another user's shows that it's waiting, moves nothing, and takes over once the first signs out.
- On Linux there's no tray and no time zone sync: setting the zone needs an administrator's approval that a program starting at sign-in can't ask for. There, the check page's rows that read the zone from the system still show yours.
- Browsers installed as a Snap or Flatpak can't start programs outside their sandbox. Alibi keeps checking once a minute in those.
- On macOS, use Whereabouts. The companion doesn't install there, because Whereabouts registers the same host.
- Its tests run in CI on Windows, macOS and Linux.

## As undetectable as it gets

Anything that changes a page's JavaScript can be spotted by a determined script. The safest cover is one where nothing is faked, because the real system already tells the same story. On Windows:

1. **A VPN for the whole computer, not just the browser,** with IPv6 tunnelled too (`::/0` next to `0.0.0.0/0`) and its kill switch on (WireGuard: **Block untunneled traffic**).
2. **Alibi Companion keeping Windows on your VPN's time zone, and Windows' Set time zone automatically off** (the tray menu opens the setting for you). Windows then carries the exit's zone, so with Match my IP address Alibi leaves the zone to Windows instead of faking it, and the tray turns red if the VPN drops or DNS or IPv6 goes around it.
3. **Alibi on Match my IP address, in Deep mode.** Deep mode gives the cover through Chrome itself, below the page. To hide its banner, start Chrome with `--silent-debugger-extension-api` (see [Modes](#modes)).
4. **Check it** on the [check page](https://doxigo.github.io/alibi/): every row should agree.

On a Mac it's the same, with Whereabouts in the companion's place, with **Match my Mac's time zone and region** on.

Some things can't be hidden. Sites can often tell that an IP address belongs to a VPN provider: they won't know where you are, but they'll know you use a VPN. Only a residential or dedicated IP avoids that. A site can also estimate how far you are from the VPN server by timing its replies.

## Modes

**Stealth Mode (default).** On every navigation, Alibi injects a script into each frame's page context. The script patches `Intl`, `Date`, `toLocaleString` and `navigator.geolocation`. No debugger is involved, so Chrome shows no banner. Limits:

- New iframes and blob Web Workers get the cover too. Workers started from the site's own files still see the real time zone. Covering them means restarting them from a blob, and on sites whose security policy forbids blob workers that fails silently, breaking the site. Third-party scripts, where fingerprinting usually comes from, can't start file workers (a worker's script must come from the page's own site), so they're limited to blob workers, which are covered. Deep covers file workers too.
- An empty iframe reached through `window.frames[n]` before anything touches its `contentWindow` is still unpatched.
- Determined fingerprinting scripts can detect the patches.

For Stealth Mode to also catch scripts that run the instant a page loads, open `chrome://extensions`, click Details on Alibi, and turn on **Allow User Scripts**. Alibi then registers its patches through `chrome.userScripts`, and Chrome runs them before any page script. With the toggle off, Alibi injects on page commit instead, and inline `<head>` scripts can still see real values when the HTML arrives within a few milliseconds of the headers. The popup shows a hint while the toggle is off.

**Deep Mode.** Turn this on in the popup. Alibi attaches `chrome.debugger` and uses the CDP `Emulation.*` overrides. Spoofing then happens below the page, which is much harder to detect, and cross-site iframes and Web Workers get the cover before their first script runs. Leaving the New Tab page (which the debugger can't touch), Alibi briefly parks the tab on a blank page to attach first, then continues to the site, so even that first page is covered from its first script. One limit remains: when you move between two sites with *different* covers in the same tab and the new page's HTML arrives almost instantly, its first inline scripts can see the previous site's cover (never your real location). Chrome holds debugger commands until the navigation commits, and re-sending the navigation could break form submissions. While it's active, Chrome shows the "started debugging this browser" banner. To use Deep Mode without the banner, launch Chrome with `--silent-debugger-extension-api` ([how to pass flags](https://www.chromium.org/developers/how-tos/run-chromium-with-flags/)). On Windows, right-click the shortcut you start Chrome from, choose **Properties**, and add the flag to the end of **Target**, after the closing quote. Quit Chrome completely first, including from the system tray, or the copy that's still running ignores the flag.

## Per-site covers

The popup has two tabs. **All sites** holds your usual cover. The second tab is named after the current site: choose **Give it its own cover** and that site, along with its subdomains, gets separate settings. The most specific rule wins, and iframes follow the page they're embedded in. To keep one site on your real location, give it its own cover and set it to **Off (real location)**. Stealth/Deep mode applies to all sites.

## IP Address

Alibi does not change your IP address. Use a VPN or proxy for that, then choose **Match my IP address**. The cover then follows your connection. While any cover uses it, Alibi asks [GeoJS](https://www.geojs.io/) where you appear to be once a minute, so switching VPN exits moves the cover within a minute. Pages already open pick up the new place when they reload.

With [Whereabouts](https://github.com/doxigo/whereabouts) running, the cover moves as soon as Whereabouts sees the new exit, and GeoJS isn't asked at all. To set it up:

1. Install Whereabouts 1.2.0 or later and open it once. Each time it starts, it registers itself with Chrome and Brave as a native messaging host (`com.doxigo.whereabouts`) that only Alibi may use.
2. Load this version of Alibi or a later one (see the note on the pinned ID under [Install](#install)).
3. Choose **Match my IP address** for all sites or for one site.

Nothing else is needed. If Whereabouts quits or isn't installed, Alibi goes back to asking GeoJS once a minute, and picks Whereabouts up again within a minute of it starting. The Chrome Web Store build leaves this out: it has a different ID and no `nativeMessaging` permission.

On Windows and Linux, [Alibi Companion](#alibi-companion-windows-and-linux) takes Whereabouts' place: install it once, and the cover moves as soon as the companion sees the new exit.

When Whereabouts also moves the Mac's time zone to the exit, open pages follow the new time zone without a reload: while the Mac's zone already matches the IP, Alibi leaves the zone to Chrome, which passes the Mac's zone changes into pages that are already open. Their language, locale and location still change on reload.

## Permission Justification

**scripting, host permissions (all sites):** Stealth Mode needs these to inject its spoofing script into pages.

**declarativeNetRequestWithHostAccess:** Sets the `Accept-Language` request header to match each site's cover.

**userScripts:** Lets Stealth Mode run before page scripts once you turn on "Allow User Scripts". Note: the Chrome Web Store limits this API to user-script managers, so a store build may need to drop it and rely on the injection fallback.

**debugger:** Deep Mode needs this to attach to tabs and emulate location, time zone, and locale.

**webNavigation:** Detects new page loads so Alibi can apply the active mode.

**storage:** Stores settings and preferences.

**alarms:** Re-checks your IP address once a minute while a cover is set to Match my IP address.

**nativeMessaging:** Alibi listens to [Whereabouts](https://github.com/doxigo/whereabouts), the companion Mac app, or to [Alibi Companion](#alibi-companion-windows-and-linux) on Windows and Linux, so a cover on Match my IP address moves the moment your VPN exit does. Alibi receives the exit's time zone, locale and coordinates. It tells the companion whether any cover is on Match my IP address, so the companion looks nothing up while none is. To a companion that can keep the system's clock on Alibi's location (Alibi Companion on Windows, Whereabouts from doxigo/whereabouts#1 on the Mac) it also sends its All sites cover's configuration, time zone and locale. The store build leaves this permission out.

## Development

Alibi is built using JavaScript and Tailwind CSS.

```
yarn
yarn build        # outputs to /build
yarn build:store  # Chrome Web Store zip: same build without the userScripts and nativeMessaging permissions or the pinned key
yarn test         # checks stealth time zone math against real TZ processes
yarn testpage     # the check page at http://localhost:8642/, with a second site and delay/policy options
```

The scripts run the same on Windows, macOS and Linux, with no `rm`, `cp` or `zip` needed. You need Node 22.2 or later and Yarn 1 (`npm install -g yarn`; recent Node releases no longer bundle Corepack). On Windows, if PowerShell refuses with "running scripts is disabled on this system", run the same commands from Command Prompt.

Alibi Companion lives in `companion/` and is written in Go (1.26 or later):

```
yarn build:companion          # the Windows and Linux programs, into companion/dist
cd companion && go test ./... # its tests
```

`companion/locales.go` is generated from Node's CLDR data, so the companion picks the same locale Alibi does, and names countries and time zones the same way. After a Node update, refresh it with `go generate` in `companion/`. `yarn build:companion` also runs `companion/cmd/winres`, which builds Alibi's stamp (from `img/icon-*.png`), the version and a manifest into the Windows programs.

### Load the Extension in Chrome

1. Open `chrome://extensions/`.
2. Enable Developer mode.
3. Click "Load unpacked".
4. Select the `build` folder, or the root folder if you're running `yarn watch:css`.

Every push and pull request runs the tests and builds on Windows, macOS and Linux. Pushes keep the unpacked extension and the companion programs as CI artifacts (Actions → the latest run on `main` → `alibi-extension`, `alibi-companion`).

### Releasing

1. Bump `version` in `manifest.json` and commit it.
2. Tag it with the release notes as the message, then push the tag:

   ```
   git tag -a v1.0.1 -m "What changed"
   git push origin v1.0.1
   ```

The Release workflow runs the tests, checks that the tag matches `manifest.json`, and publishes a GitHub Release with `alibi-v1.0.1.zip` (for Load unpacked), `alibi-v1.0.1-chrome-web-store.zip` (for the store), and Alibi Companion for Windows and Linux with a `SHA256SUMS` file.
