# Chrome Web Store listing

Upload `alibi-store.zip` from `yarn build:store`. That build leaves out the `userScripts` permission, because the Web Store reserves it for running scripts users write. Stealth mode then uses its injection fallback.

## Store listing

**Name** (from `_locales/*/messages.json`): Alibi - Location Guard & Locale Switcher

**Summary** (from `_locales`): Spoof time zone, geolocation & locale. Match your location data to your VPN's IP address.

**Category:** Privacy & Security

**Language:** English

**Description:**

> Your browser was somewhere else.
>
> Alibi makes websites see the time zone, language and location you pick, so they tell the same story as your VPN. Pick one of 44 places, match your IP address in one click, or type your own time zone, locale and coordinates.
>
> One consistent story
> • Time zone and clock, date and number formats, navigator.language, the Accept-Language header and geolocation all agree.
> • Iframes and the workers that scripts start follow the page they belong to.
>
> A different cover for each site
> • Give any site its own place. Its subdomains follow it.
> • Keep a site, such as your bank, on your real location.
>
> Two modes
> • Stealth (default): rewrites what each page reads. Chrome shows no banner.
> • Deep: works below the page through Chrome's debugger, so it's harder to detect. Chrome shows a "started debugging this browser" banner while it's on.
>
> Alibi doesn't change your IP address; use a VPN or proxy for that. It has no analytics or tracking and keeps your settings in your browser. It only contacts a server (GeoJS, over HTTPS) when you choose "Match my IP address".
>
> Free and open source (GPL-3.0), based on Cloaq.

**Images** (in this folder):

| Slot | File | Size |
|---|---|---|
| Icon | `../img/icon-128.png` | 128×128 |
| Screenshot 1 | `screenshot-1.png` | 1280×800 |
| Screenshot 2 | `screenshot-2.png` | 1280×800 |
| Screenshot 3 | `screenshot-3.png` | 1280×800 |
| Small promo tile | `tile-small.png` | 440×280 |
| Marquee promo tile | `marquee.png` | 1400×560 |

To change an image, edit its page in `src/` and re-render it at the size above. The screenshots embed real popup captures from `src/`.

## Privacy practices

**Single purpose:** Make websites see the time zone, language and location the user picks, so they match the user's VPN or test location.

**Permission justifications:**

- **host permissions (all sites):** Spoofing has to reach every page and frame the user visits, and the Accept-Language header has to change on requests to every site.
- **scripting:** Injects the script that makes pages read the chosen time zone, locale, language and location.
- **declarativeNetRequestWithHostAccess:** Sets the Accept-Language request header to the chosen language, per site.
- **debugger:** Only for Deep mode, which the user turns on. Applies Chrome's built-in time zone, locale, language and geolocation emulation to the user's tabs.
- **webNavigation:** Detects page loads so the right cover (all sites, or a site's own) applies before the page reads it.
- **storage:** Saves the user's settings locally.

**Remote code:** No. All code ships in the package.

**Data usage:** Location (IP-based): only when the user chooses "Match my IP address", the extension requests the connection's approximate location from GeoJS and stores the result locally as the user's settings. It is not sold, not used for anything unrelated to this purpose, and not used for creditworthiness.

**Privacy policy URL:** https://github.com/doxigo/alibi/blob/main/PRIVACY.md
