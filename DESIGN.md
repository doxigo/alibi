---
name: Alibi
description: A postmark for your browser. Envelope stock, cancellation ink and the airmail chevron.
colors:
  paper: "#FAFBFB"
  ink: "#161A23"
  ink-2: "#4A5263"
  ink-3: "#636B7C"
  rule: "#D0D5DE"
  airmail-red: "#C41E2A"
  red-fill: "#C41E2A"
  airmail-blue: "#1F3F91"
  label: "#1F3F91"
  print-white: "#FFFFFF"
  night-paper: "#13204A"
  night-ink: "#ECF0F6"
  night-ink-2: "#B6BECD"
  night-ink-3: "#919BAD"
  night-rule: "#384A7E"
  night-red: "#FF6368"
  night-blue: "#96B2FF"
  night-label: "#2B4FB0"
typography:
  display:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "40px"
    fontWeight: 800
    lineHeight: 1.02
    letterSpacing: "-0.025em"
    fontVariation: "'wdth' 86"
  headline:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "28px"
    fontWeight: 800
    lineHeight: 1.05
    letterSpacing: "-0.02em"
    fontVariation: "'wdth' 84"
  title:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "21px"
    fontWeight: 700
    letterSpacing: "-0.01em"
    fontVariation: "'wdth' 92"
  wordmark:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "19px"
    fontWeight: 800
    letterSpacing: "-0.02em"
  lede:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "17px"
    fontWeight: 400
    lineHeight: 1.55
  body:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 400
    lineHeight: 1.6
  field:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 500
    fontFeature: "'tnum' 1"
  ui:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "13.5px"
    fontWeight: 650
  meta:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "12.5px"
    fontWeight: 600
  label:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "11px"
    fontWeight: 600
    letterSpacing: "0.07em"
    fontVariation: "'wdth' 88"
  die-caps:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 700
    letterSpacing: "1.6px"
    fontVariation: "'wdth' 72"
  die-time:
    fontFamily: "Archivo, system-ui, sans-serif"
    fontSize: "25px"
    fontWeight: 700
    fontFeature: "'tnum' 1"
    fontVariation: "'wdth' 80"
rounded:
  none: "0px"
  label: "2px"
  code: "3px"
  control: "4px"
spacing:
  hairline-gap: "6px"
  button-x: "14px"
  stack: "12px"
  gutter: "20px"
  section: "44px"
components:
  button-ink:
    textColor: "{colors.ink}"
    typography: "{typography.ui}"
    rounded: "{rounded.control}"
    padding: "0 14px"
    height: "36px"
  button-ink-hover:
    backgroundColor: "rgba(22, 26, 35, 0.06)"
  button-quiet:
    textColor: "{colors.ink-2}"
    rounded: "{rounded.control}"
    padding: "0 8px"
    height: "32px"
  button-quiet-pending:
    backgroundColor: "{colors.red-fill}"
    textColor: "{colors.print-white}"
  button-link:
    textColor: "{colors.ink-2}"
    typography: "{typography.meta}"
  button-link-hover:
    textColor: "{colors.airmail-red}"
  input-address-line:
    textColor: "{colors.ink}"
    typography: "{typography.field}"
    rounded: "{rounded.none}"
    padding: "6px 0 7px"
  tab-scope:
    textColor: "{colors.ink-2}"
    typography: "{typography.ui}"
    padding: "10px 0 9px"
  tab-scope-active:
    textColor: "{colors.ink}"
  switch-halves:
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "6px 14px 7px"
  switch-halves-active:
    backgroundColor: "{colors.red-fill}"
    textColor: "{colors.print-white}"
  postal-label:
    backgroundColor: "{colors.label}"
    textColor: "{colors.print-white}"
    rounded: "{rounded.label}"
    padding: "9px 14px"
  airmail-edge:
    height: "6px"
  cancellation-die:
    textColor: "{colors.ink}"
    size: "128px"
---

# Design System: Alibi

## Overview

**Creative North Star: "The Cancellation Mark"**

A postmark is the original alibi: proof that a letter was in one city at one time. Alibi strikes that proof for the browser. The world is a piece of airmail. Envelope-white stock, blue-black cancellation ink, and a red, white and blue chevron edge along the top. The one live object is a circular cancellation die that names the place and keeps its local time. It is struck slightly askew, the way a hand stamp lands, and the ink is visibly imperfect.

Density is that of a postal form: small, exact labels over typed lines, everything set in one variable family (Archivo) and moved along its width axis rather than swapped for a second face. Condensed caps belong to the die, near-normal widths to the interface, tabular figures to anything that counts. Colour is mostly ink on stock. Red marks state and blue marks the post office's own voice (links, the gummed label). At night the envelope turns to airmail navy and the ink to paper white, but the printed chevron does not change.

The system rejects the category default it was built against: no dark VPN panel, no shield, no big on/off toggle. Depth comes from ink and paper, not from cards and shadows.

**Key Characteristics:**
- Envelope stock and cancellation ink, with a night-airmail navy scheme.
- A fixed airmail chevron edge as the brand stripe, at the top of every surface.
- One variable family, Archivo, used across its width axis (72% to 100%).
- Fields typed onto hairline rules, never boxed.
- A cancellation die with a live clock, per-place tilt, an ink-impression mask and a lift-then-strike re-stamp.
- Red for the one active state, blue for links and the gummed label.

## Colors

Two inks on envelope stock, plus the printed airmail red and blue. Everything is a CSS custom property holding an RGB triplet (for Tailwind alpha syntax), switched by `prefers-color-scheme`.

### Primary
- **Airmail Red** (`airmail-red`, light): red text and every focus ring. It is the caret colour in fields, the focused or invalid underline of an address line, error copy, the selection tint (at 22% alpha), and the step numerals on the guide.
- **Printed Airmail Red** (`red-fill`): filled active states only. The active half of the Stealth | Deep switch, the scope-tab underline, and the reload button when a change is waiting. It holds the same value in both schemes because it is a printed ink, not a theme colour.

### Secondary
- **Airmail Blue** (`airmail-blue`): links in running text on the guide.
- **Label Blue** (`label`): the ground of the gummed postal label, and the tile of the mark and logo.

### Neutral
- **Envelope Stock** (`paper`): the page and popup ground.
- **Cancellation Ink** (`ink`): primary text, the die, the cancel lines, and 1.5px button and switch borders.
- **Faded Ink** (`ink-2`): secondary text. Field labels, the lead and meta lines of the strike, inactive tabs, quiet buttons, and guide body copy.
- **Pale Ink** (`ink-3`): placeholders, disabled tabs, the "· all sites" qualifier, and the guide footer. It is also the hover colour of an address line's rule.
- **Hairline** (`rule`): the 1px rule under fields and tabs, and section dividers.
- **Print White** (`print-white`): the white bars of the chevron, and text on red fill and on the label. It is fixed in both schemes.

### Night airmail (dark scheme)
- **Airmail Navy** (`night-paper`) replaces stock. **Paper-White Ink** (`night-ink`), `night-ink-2` and `night-ink-3` replace the inks. The **Night Rule** (`night-rule`) is a lighter navy hairline.
- **Night Red** (`night-red`) is the lighter red for red text and focus rings on navy. It is never a fill.
- **Night Blue** (`night-blue`) is the lighter link blue. **Night Label** (`night-label`) is the deeper printed blue the gummed label and mark tile use at night.

### Named Rules
**The Fill-Versus-Ink Rule.** Fills use printed values and text uses legible values. Filled red is always `red-fill` with white text, in both schemes. The lighter `night-red` is only ever text or a ring. The label works the same way: a deep printed blue ground, a lighter blue for link text.

**The Printed Stock Rule.** The airmail chevron is printed on the envelope, so it is red `#c41e2a`, white and blue `#1f3f91` in both schemes. Never theme it.

**The State-Red Rule.** Red marks state: the active choice, a pending action, focus and errors. Ordinary content stays in ink. On the guide, red numbers the steps of a procedure and does nothing else.

## Typography

**Display Font:** Archivo variable (wght 100–900, wdth 62–125), self-hosted from `fonts/archivo.woff2`, with system-ui and sans-serif as fallbacks.
**Body Font:** Archivo, the same file.

**Character:** One grotesque used across its width axis. Narrow and heavy on the die, slightly condensed and extra-bold for display, normal width for the interface. The width changes do the work a second family would do elsewhere.

### Hierarchy
- **Die caps** (700, 13px in die units, 72% width, tracked 1.6, uppercase): the city on the upper arc. The country and code on the lower arc are 600 at 9.5px. The date and offset inside are 600, 9–10px at 75% width.
- **Die time** (700, 25px in die units, 80% width, tabular): the live local time at the centre of the die.
- **Display** (800, 40px, 1.02, −0.025em, 86% width, balanced): the guide's single headline.
- **Headline** (800, 28px, 1.05, −0.02em, 84% width): the place name in the popup strike, truncated to one line.
- **Title** (700, 21px, −0.01em, 92% width, balanced): guide section heads, with 44px above them.
- **Wordmark** (800, 19px, −0.02em): "alibi" in lowercase beside the mark in the popup header.
- **Lede** (400, 17px, 1.55, max 46ch): the guide's one-sentence promise.
- **Body** (400, 16px, 1.6, max 34rem, about 70 characters): guide prose in faded ink. Strong text steps up to ink at 650.
- **Field** (500, 15px, tabular): typed values in address lines. Placeholders drop to 400 in pale ink.
- **UI** (650, 13.5px): tabs, ink buttons, the mode heading and switch titles. The switch's full-size titles are 700 at 15px.
- **Meta** (500–600, 12.5px): the strike's lead and meta lines, error lines, link buttons and the postal label.
- **Label** (600, 11px, 0.07em, 88% width, uppercase): field labels, set like the small print on a postal form.

### Named Rules
**The Width Axis Rule.** Hierarchy moves along `wdth` as well as weight: 72–80% for the die, 84–86% for display, 88–92% for labels and titles, 100% for the interface. Do not bring in a second family for contrast.

**The Counting Figures Rule.** Times, dates, UTC offsets, coordinates, locales in the meta line, version numbers and step numerals use tabular figures (`font-variant-numeric: tabular-nums`). Running prose turns them off.

## Layout

The popup is a fixed 400px column with a 20px gutter. From top to bottom: the 6px chevron edge, a header row (mark and wordmark on the left, quiet actions on the right), the strike, scope tabs, the address block, and the mode row. Fields stack at 12px. Time zone, locale, latitude and longitude sit in a two-column grid with 20px between the columns. The strike puts the 128px die on the left. Wavy cancel lines run from its shoulder to the right edge, and the lead, place and meta lines sit in a band below the lines, not over them.

The guide is a centred 680px column with 24px side padding and a 10px chevron edge at top and bottom. Its opening pairs a 168px die (tilted −6°) with the display headline in an auto/1fr grid that stacks into one column below 560px. Prose is capped at about 70 characters.

**The 600px Rule.** Chrome caps a popup at 600px tall, so the popup's tallest state, the User Scripts label plus a field error, must fit in 600px without scrolling. This is why the die is 128px and not larger. Check every addition against that state.

**The One Notice Rule.** The popup shows at most one notice at a time. An error (a field error or a failed IP lookup) outranks the User Scripts label, which comes back once the error clears. Never stack a label on top of an error.

## Elevation & Depth

Flat. Nothing in the system casts a shadow or floats on a card. Depth is the depth of ink on paper. The die and the cancel lines are masked through scanned-style impression textures (`img/ink-die.png`, `img/ink-lines.png`) and run through a slight bleed filter (a 0.3 Gaussian blur, then an alpha curve with slope 2.4 and intercept −0.45), so edges read as pressed ink. `box-shadow` appears only as a line: the 1px focus underline on an address line and the 2px inset underline of the active tab.

**The Ink Not Elevation Rule.** If something needs to stand apart, change its ink, rule or fill. Never lift it with a shadow.

## Shapes

The geometry is rectilinear, with small corners. Controls (ink buttons, quiet buttons, the halves switch) use a 4px corner. The postal label uses 2px and inline code 3px. Address lines have no corners because they have no box. Borders are either a 1px hairline in `rule` (under fields, tabs and sections) or a 1.5px ink stroke (ink buttons, the halves switch, the guide's two-mode panel). The only round shapes are the die and the mark's stamp circle. The only patterned edges are the chevron's −48° bars (9px colour, 5px white) and the label's perforation.

## Components

### Buttons
Stamped, not glossy. Ink outlines and plain type, with red only when something is pending.
- **Ink button:** transparent with a 1.5px ink border, 4px corner, 36px tall, 14px side padding, UI type. On hover it gets a 6% ink wash. Used for a single deliberate action such as "Give it its own cover".
- **Quiet button:** borderless, faded ink, 32px tall, 8px padding, 13px at 600, with a 16px stroked SVG icon (stroke width 1.8). Hover moves to ink with a 6% ink wash. When a change is waiting to apply, it fills with printed red and white text and its label reads "Reload to apply". This is the popup's primary action.
- **Link button:** faded-ink meta text, underlined 1px at a 0.18em offset. As built, it hovers to red, not blue. It is used for secondary removals ("Remove this site's cover").
- **Focus:** every control gets a 2px red outline at a 2px offset (`night-red` at night).

### Inputs / Fields
**Address lines.** A typed line on a hairline, with no box and no fill.
- **Style:** transparent, a 1px `rule` underline, 6px above and 7px below the text, field type, and a red caret.
- **Hover:** the rule darkens to pale ink.
- **Focus and invalid:** the rule turns red and thickens with a 1px red underline shadow (160ms ease-out). Invalid is the same red line plus one meta-size error line in red below the block.
- **Select:** the same line with a 16px stroked chevron-down icon at the right and 28px of right padding. Options are drawn on stock in ink.
- **Label:** a field label sits above every line.

### Navigation
**Scope tabs.** Two text tabs, "All sites" and the current host (truncated at 210px), 20px apart on a hairline.
- **Default:** faded ink, UI type, 10px above and 9px below.
- **Active:** ink, with a 2px printed-red underline drawn as an inset shadow that eases in over 200ms.
- **Disabled:** pale ink with a not-allowed cursor.

### Stealth | Deep switch
Two hard halves of one 1.5px ink-bordered box, split by a 1.5px ink line, 4px corner. A printed-red fill sits behind the checked half and slides across in 260ms (`cubic-bezier(0.16, 1, 0.3, 1)`). The checked half's text turns white. Each half is a label wrapping a visually hidden radio, so the arrow keys work, and focus draws a red outline at a 3px offset. It pads 6px by 14px with 13.5px/700 titles centred in equal halves, sized for the popup's mode row. The split line sits on the seam above the fill, and the fill keeps the box's 2.5px inner corners, so both states mirror each other. The unchecked half takes the ink button's 6% ink wash on hover. On the guide, the two-mode panel reuses the bordered halves statically, with no fill. The popup's WebRTC row (Open | Sealed) is the same switch, under its own rule line below the mode row.

### Postal label
A gummed label with a perforated top edge (a radial-gradient mask of 2.6px holes every 10px). It has a label-blue ground, white meta text at 1.45 line height, a 2px corner, and 9px by 14px padding. Its inline action is a semibold, underlined button. It is the popup's only advisory surface, and it obeys the One Notice Rule.

### Airmail edge
A full-width band of repeating −48° bars: red 9px, white 5px, blue 9px, white 5px. It is 6px tall in the popup and 10px on the guide, and it is decorative (`aria-hidden`).

### Cancellation die
The signature. An SVG on a 140-unit viewBox, drawn in `currentColor` (ink). It has an outer ring (2.6 stroke), an inner ring (1.4 stroke) and two dots at 9 and 3 o'clock. The city sits on the upper arc and the country and code on the lower arc, in die caps. Inside are the date (`18 OCT 26`), the live time and the UTC offset. All of it is masked by the ink-impression texture and the bleed filter.
- **Tilt:** each place hashes to its own repeatable rotation between −7° and +3°, set through `--die-rot` (the default is −4°).
- **Re-strike:** when the place changes, a copy of the old impression lifts off (140ms, accelerating: fades, rises 6px, scales to 1.06, blurs 1.5px). The new die then presses down after a 90ms delay (320ms, `cubic-bezier(0.16, 1, 0.3, 1)`): it lands from 1.14 scale, 5° over-rotated and blurred, dips to 0.97 and settles.
- **Reduced motion:** there is no lift and no strike, and the die swaps instantly. The halves fill stops sliding too.
- **Sizes:** 128px in the popup and 168px on the guide.

### Wavy cancel lines
Five parallel machine-cancel waves (1.6 stroke, 8px apart, a 16px period) in ink. They use the same bleed filter and their own impression mask (`ink-lines.png`), and run from the die's shoulder to the popup's right edge. They stay above text and never run behind it.

### Mark and logo
A 24px label-blue tile with a 5px corner. On it, a white stamp circle and three white waves: the die and its cancel lines reduced to an icon. The logo adds the "alibi" wordmark in ink. At night both switch the tile to `night-label` and the ink to paper white. Toolbar icons are rasterised from `img/src/icon-small.svg` (16px and 32px) and `img/src/icon-large.svg` (48px and 128px).

## Do's and Don'ts

### Do:
- **Do** put the airmail chevron edge at the top of every surface (6px in the popup, 10px on full pages), in its fixed printed red, white and blue.
- **Do** fill active states with `red-fill` and white text in both schemes. Keep `airmail-red` and `night-red` for red text, carets and focus rings.
- **Do** set fields as address lines: an 11px uppercase label above a typed line on a 1px hairline that turns red on focus.
- **Do** use tabular figures for every time, date, offset, coordinate and step number.
- **Do** keep the popup's tallest state within 600px, and show one notice at a time.
- **Do** give every motion a reduced-motion fallback that swaps state instantly.

### Don't:
- **Don't** box inputs, put them on cards, or give them a filled background.
- **Don't** theme the chevron edge, or use the lighter `night-red` as a fill.
- **Don't** use shadows for elevation. A shadow in this system is only ever a 1–2px line.
- **Don't** run cancel lines or the impression texture behind text that must be read.
- **Don't** add a second typeface. Vary Archivo's width and weight instead.
- **Don't** fall back to the category default this world replaces: a dark VPN panel, a shield, a big on/off toggle.
