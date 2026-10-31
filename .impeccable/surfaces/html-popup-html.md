---
version: 1
slug: "html-popup-html"
primary_target: "html/popup.html"
related_targets: ["html/info.html"]
---

## Scope

Toolbar popup (`html/popup.html`, fixed 400px wide) is the primary surface, mode Operate. The install/options page (`html/info.html`, mode Read) and the icon/wordmark (`img/`) share the same world.

## Task

Glance at what sites currently see (place, time zone, local time, locale), switch location in one or two clicks (preset, Match IP, or custom fields), choose Stealth or Deep, reload. States: off (Browser Default), active preset, custom, Match IP (loading/failed), Stealth with User Scripts off (hint), Deep active (banner warning).

## Direction contract

THESIS: A postmark is the original alibi, proof that a letter was in one city at one time; the popup strikes that proof for your browser. It refuses the category default of a dark VPN panel with a shield and a big on/off toggle.

OWN-WORLD: Envelope-white stock (dark scheme: night airmail navy with paper-white ink) and blue-black cancellation ink. The airmail chevron edge (red, white, blue diagonal bars) is the brand stripe. Airmail red marks the one active choice and focus; airmail blue marks links and secondary actions. Archivo variable throughout: condensed caps on the die, normal width for UI, tabular figures for times and coordinates. Fields are typed address lines on hairline rules, not boxed inputs. The notice is a gummed postal label with a perforated edge.

STORY: The visitor sees at once where sites think they are and what local time they report, trusts that the zone, locale and coordinates agree, changes the place, and knows whether Chrome will show a banner.

FIRST VIEWPORT: An airmail chevron edge runs along the top. The header row holds the wordmark and the reload/info actions. The strike follows: a circular die about 136px across on the left, rotated a few degrees per place, with the city on the upper arc, country and code on the lower arc, and the date and live time inside; wavy machine-cancel lines run right to the popup edge. Over the lines sit "Sites see you in", the place name at display size, and the UTC offset and locale. Below that is the address block of fields, then the Stealth | Deep two-half switch. The primary action, reload to apply, sits in the header.

FORM: Cancellation Mark (postal cancellation dies and airmail envelopes), the IMPECCABLE'S PICK card and #1 on the ordered list. Seed key cd7bf583. Signature interaction: changing place re-strikes the die (the old impression lifts, and the new one lands with a short press and a slight rotation settle). Under reduced motion it swaps instantly.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Cited adaptations

- The die is 128px, not about 136px: Chrome caps popups at 600px tall, and the tallest state (User Scripts label plus a field error) has to fit.
- The lead, place and meta line sit in a band below the cancel lines rather than over them. At 400px, wavy lines behind the 28px place name cut its contrast, and legibility outranks the overlay.
- Per-site scope tabs (All sites, or the current host) sit between the strike and the address block. The user asked for per-site settings mid-build.
- Dark scheme: filled active states use printed airmail red #c41e2a with white text. The lighter red is reserved for red text and focus rings, where navy needs the extra contrast.
- WebRTC (Open | Sealed, asked for after the build) is one line of customs-form tick boxes under the mode row, with an envelope that seals and an ink helper slip per box. A second stamped switch row read as a second Mode and pushed the tallest state to 640px. The line keeps it at 597px.

## Unresolved

- No real store screenshots exist yet; promo art must be produced later from the built UI.
