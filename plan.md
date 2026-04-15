# CSS

## requirements
- [ ] bare minimum CSS styling for alpha testing
  - no redundant classes
  - measure everything in em
  - maximally abstracted
- [ ] consistent use of `<article>` and `<div>`
- [ ] add classes where missing and necessary

## plan

### 1. strip the debug universal selector
`static/css/style.css:1-8` paints every element with a dotted border, grey background,
rebeccapurple text, and rounded corners. That's leftover dev scaffolding, not styling —
remove it. Keep only the true reset (`*, *::before, *::after { box-sizing: border-box }`
and `* { margin: 0 }`), and collapse the duplicate `*` rules into one block.

### 2. normalize units to em
Audit `style.css` for non-`em` lengths and convert:
- `min-height: 100vh` on `body` → keep (viewport units are correct here, not a size measurement)
- any `rem`, `px`, `%` used for spacing/sizing → `em`
- shadows and hairlines (`1px` borders, `0 .5em ...` shadows) → leave `px` only for sub-pixel
  hairlines; everything else in `em`
Set a single root `font-size` on `body` so `em` scales predictably.

### 3. kill redundant rules and classes
- `.player { background: white }` + `.player div { background: white }` → one rule
- `.card` + `.player` + `.accuse-panel` all set `background: white` → extract a `.surface`
  utility (or a single shared selector) used by each
- `.button` is currently one visual style with a huge `padding: 3em`; buttons in dialogs
  and action bars need the same look. Keep one `.button` rule, drop the ad-hoc sizing.
- remove the commented-out blocks (`.players` position/min-height, `#table { display: none }`)

### 4. consistent `<article>` vs `<div>` in templates
Rule of thumb to apply across `static/html/tmpl.*.html`:
- `<article>` = a self-contained unit of content (a card, a player panel, the table panel,
  a dialog body)
- `<section>` = a labeled region of the page (players list, table, points, status)
- `<div>` = presentational wrapper only (flex/grid container, no semantic meaning)

Specific fixes:
- `tmpl.players.html`: `div.card-float > div.card > article#players.players` — the outer
  `div.card` should be `<article class="card">` (matches `index.html`), and the inner
  `article#players` should be a `<section>` since it's a labeled list region.
- `tmpl.table.html`: `<article id="table">` lives inside `<section id="table">` in
  `tmpl.game.html`. Rename inner to `<div class="table-actions">` — it's a button row, not
  an article.
- `tmpl.accuse_dialog.html`: wrap the per-player blocks in `<article class="accuse-option">`
  instead of bare `<div>`.
- `tmpl.points.html`: `<article class="action">` wrapping a single button → `<div>`.

### 5. add classes where missing
Currently styled by id or tag; give them classes so styles are reusable:
- `#status`, `#initiative` footers → `.status`, `.initiative`
- `#players`, `#table`, `#points` sections → keep ids for htmx targets, add
  `class="panel"` for shared panel styling
- dialog bodies (`#accuse-content`, `#modifier-content`) → `class="dialog-body"`
- action button rows → `class="actions"` (replaces the current mix of `.action` singular
  and inline groupings)

### 6. abstract the shared primitives
End state — a small set of composable classes:
- `.panel` — flex column container with gap/padding (replaces duplicated `.players`,
  `.card`, `.accuse-panel` layout rules)
- `.surface` — white background + shadow (the "card-looking" look)
- `.button` — one button style, sized by context via `em` padding
- `.actions` — horizontal button row
- `.stack` — vertical flex with `gap: .75em` (replaces ad-hoc `display: flex; flex-direction: column; gap`)

### execution order
1. Rewrite `style.css` around the primitives above (§1, §2, §3, §6).
2. Sweep templates to apply new classes and fix `<article>`/`<div>`/`<section>` semantics (§4, §5).
3. Load each page (`/`, `/:id/join`, `/:id`) in the browser and verify nothing regressed —
   especially dialogs, the modifier card flow, and the htmx polling targets.
