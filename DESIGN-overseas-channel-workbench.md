---
version: alpha
name: Overseas Channel Workbench Analysis
description: An analysis of the Overseas Channel Workbench design language — a calm, data-dense enterprise BI system built on a near-white canvas of soft gray, structured by hairline-bordered white cards and lit by a single confident Blurple-family brand blue. Clean Outfit sans throughout, generous 16px card radii, whisper-soft shadows reserved for overlays, and a full-parity dark mode on a cool indigo-gray. The mood is precise, professional, and analytical — a control room, never an arcade.

colors:
  primary: "#465fff"
  primary-hover: "#3641f5"
  on-primary: "#ffffff"
  primary-soft: "#ecf3ff"
  success: "#12b76a"
  error: "#f04438"
  warning: "#f79009"
  info: "#0ba5ec"
  accent-purple: "#7a5af8"
  accent-pink: "#ee46bc"
  canvas: "#f9fafb"
  surface: "#ffffff"
  surface-dark: "#1a2231"
  canvas-dark: "#101828"
  ink: "#1d2939"
  ink-strong: "#101828"
  ink-muted: "#667085"
  ink-subtle: "#98a2b3"
  hairline: "#e4e7ec"
  hairline-strong: "#d0d5dd"

typography:
  title-2xl:
    fontFamily: Outfit
    fontSize: 72px
    fontWeight: 700
    lineHeight: 90px
    letterSpacing: 0
  title-lg:
    fontFamily: Outfit
    fontSize: 48px
    fontWeight: 700
    lineHeight: 60px
    letterSpacing: 0
  title-md:
    fontFamily: Outfit
    fontSize: 36px
    fontWeight: 700
    lineHeight: 44px
    letterSpacing: 0
  title-sm:
    fontFamily: Outfit
    fontSize: 30px
    fontWeight: 600
    lineHeight: 38px
    letterSpacing: 0
  kpi-value:
    fontFamily: Outfit
    fontSize: 22px
    fontWeight: 700
    lineHeight: 28px
    letterSpacing: 0
  theme-xl:
    fontFamily: Outfit
    fontSize: 20px
    fontWeight: 600
    lineHeight: 30px
    letterSpacing: 0
  body:
    fontFamily: Outfit
    fontSize: 16px
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: 0
  theme-sm:
    fontFamily: Outfit
    fontSize: 14px
    fontWeight: 500
    lineHeight: 20px
    letterSpacing: 0
  theme-xs:
    fontFamily: Outfit
    fontSize: 12px
    fontWeight: 500
    lineHeight: 18px
    letterSpacing: 0

rounded:
  xs: 4px
  sm: 6px
  md: 8px
  lg: 12px
  xl: 16px
  2xl: 24px
  3xl: 24px
  pill: 9999px
  full: 9999px

spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 20px
  xl: 24px
  xxl: 32px
  section: 40px

components:
  sidebar:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.theme-sm}"
    borderColor: "{colors.hairline}"
    padding: "{spacing.md}"
    width: "290px | 90px"
  nav-item:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    typography: "{typography.theme-sm}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xs} {spacing.sm}"
  nav-item-active:
    backgroundColor: "{colors.primary-soft}"
    textColor: "{colors.primary}"
    typography: "{typography.theme-sm}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xs} {spacing.sm}"
  header:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    padding: "{spacing.sm} {spacing.md}"
  app-switcher-pill:
    backgroundColor: "{colors.canvas}"
    activeBackground: "{colors.surface}"
    textColor: "{colors.ink-muted}"
    activeTextColor: "{colors.ink-strong}"
    rounded: "{rounded.pill}"
    padding: "{spacing.xs} {spacing.md}"
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.theme-sm}"
    rounded: "{rounded.lg}"
    padding: "{spacing.sm} {spacing.lg}"
    shadow: "shadow-theme-xs"
  button-outline:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline-strong}"
    typography: "{typography.theme-sm}"
    rounded: "{rounded.lg}"
    padding: "{spacing.sm} {spacing.lg}"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    rounded: "{rounded.xl}"
    padding: "{spacing.lg} {spacing.xl}"
  component-card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    rounded: "{rounded.xl}"
    padding: "{spacing.xl}"
  kpi-card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink-strong}"
    borderColor: "{colors.hairline}"
    typography: "{typography.kpi-value}"
    rounded: "{rounded.xl}"
    padding: "{spacing.sm} {spacing.md}"
  input:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline-strong}"
    typography: "{typography.theme-sm}"
    rounded: "{rounded.lg}"
    padding: "10px {spacing.md}"
    height: "44px"
    focusRing: "{colors.primary}/20"
  badge-light:
    backgroundColor: "{colors.primary-soft}"
    textColor: "{colors.primary}"
    typography: "{typography.theme-xs}"
    rounded: "{rounded.pill}"
    padding: "2px {spacing.sm}"
  badge-solid:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.theme-xs}"
    rounded: "{rounded.pill}"
    padding: "2px {spacing.sm}"
  modal:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.2xl}"
    padding: "{spacing.xl}"
    shadow: "shadow-theme-xl"
    overlay: "{colors.ink-subtle}/50 + backdrop-blur"
  dropdown:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    rounded: "{rounded.xl}"
    padding: "{spacing.xs}"
    shadow: "shadow-theme-lg"
  table-header:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.theme-xs}"
    borderColor: "{colors.hairline}"
    padding: "{spacing.sm} {spacing.lg}"
  table-row:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.theme-sm}"
    borderColor: "{colors.hairline}"
    padding: "{spacing.sm} {spacing.lg}"
  filter-bar:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    rounded: "{rounded.xl}"
    padding: "{spacing.md}"

  # ─── Examples (illustrative) — kit-mirror surfaces mapped to this project's primitives ───
  ex-pricing-tier:
    description: "Default card tier — re-uses component-card chrome on the white surface with a hairline border."
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    borderColor: "{colors.hairline}"
    rounded: "{rounded.xl}"
    padding: "{spacing.xl}"
  ex-pricing-tier-featured:
    description: "Featured tier — brand-soft fill with a brand border to promote the highlighted plan."
    backgroundColor: "{colors.primary-soft}"
    textColor: "{colors.primary}"
    borderColor: "{colors.primary}"
    rounded: "{rounded.xl}"
    padding: "{spacing.xl}"
  ex-product-selector:
    description: "Summary / What's Included card — component-card chrome with a divided list."
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.xl}"
    padding: "{spacing.xl}"
  ex-cart-drawer:
    description: "Right-side summary drawer — surface fill, hairline item dividers."
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.xl}"
    padding: "{spacing.xl}"
    item-divider: "{colors.hairline}"
  ex-app-shell-row:
    description: "Sidebar nav row inside the app shell. Active state = brand-soft fill + brand text."
    backgroundColor: "transparent"
    activeIndicator: "{colors.primary-soft}"
    rounded: "{rounded.lg}"
    padding: "{spacing.xs} {spacing.sm}"
  ex-data-table-cell:
    description: "Default data-table th + td chrome. Header uses canvas fill + muted xs caps; body uses theme-sm."
    headerBackground: "{colors.canvas}"
    headerTypography: "{typography.theme-xs}"
    bodyTypography: "{typography.theme-sm}"
    cellPadding: "{spacing.sm} {spacing.lg}"
    rowBorder: "{colors.hairline}"
  ex-auth-form-card:
    description: "Sign-in / sign-up card — component-card chrome wrapping the input + button primitives."
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.xl}"
    padding: "{spacing.xl}"
  ex-modal-card:
    description: "Modal dialog surface — 24px radius, xl shadow, blurred gray overlay."
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.2xl}"
    padding: "{spacing.xl}"
  ex-empty-state-card:
    description: "Empty-state frame — component-card chrome, centered caption in muted ink."
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.xl}"
    padding: "{spacing.section}"
    captionTypography: "{typography.body}"
  ex-toast:
    description: "Toast / alert strip — surface fill, semantic left accent, sm shadow."
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.lg}"
    padding: "{spacing.sm} {spacing.md}"
    typography: "{typography.theme-sm}"

---


## Overview

The Overseas Channel Workbench is a data-dense BI and admin console, and its design is quiet on purpose. Pages sit on a near-white canvas (`{colors.canvas}` — #f9fafb) and organize themselves into crisp white cards (`{colors.surface}`) separated by 1px gray hairlines. Where a marketing site shouts, this system whispers: no gradients, no full-bleed colour bands, no oversized display type competing with the data. The interface gets out of the way so that KPI numbers, cross-tab tables, and time-series charts can carry the meaning. The whole thing reads like a control room — measured, legible, professional.

The brand anchor is a single **Blurple-family brand blue** (`{colors.primary}` — #465fff). It owns exactly one job on each surface: the primary CTA, the active sidebar item, the focus ring, the selected filter, the first series in every chart. It is used sparingly and never decoratively — when blue appears, it means "this is the action" or "this is selected." Around it sits a disciplined semantic set — green for positive deltas, red for negative, amber for warnings, cyan for info — that appears only to encode data state (a rising KPI, a failed crawl), not for flourish. Everything else is neutral gray.

Geometry is soft but restrained. Controls and inputs round at `{rounded.lg}` (12px); cards and panels at `{rounded.xl}` (16px); modals reach `{rounded.2xl}` (24px). There is no toy-like `40px+` bowing here — the radii are just generous enough to feel modern and approachable without undermining the analytical tone. Depth is equally reserved: cards are flat, bordered by hairlines rather than lifted by shadow; the soft `shadow-theme-*` elevations are held back for things that genuinely float — dropdowns, modals, tooltips, date pickers.

A first-class **dark mode** mirrors every surface. The canvas drops to `{colors.canvas-dark}` (#101828), cards become a translucent white wash (`white/[0.03]`) over a cool indigo-gray (`{colors.surface-dark}` — #1a2231), and text inverts to `white/90`. Dark mode is not an afterthought skin — every component ships both polarities via Tailwind's `dark:` variant.

**Key Characteristics:**
- Near-white `{colors.canvas}` structured by hairline-bordered white cards — never a coloured or gradient background.
- One iconic brand colour: brand blue `{colors.primary}` owns the primary action and selected state, and nothing else.
- A strict semantic palette (green/red/amber/cyan) used only to encode data state, never as decoration.
- Clean Outfit sans across the whole system — no display/body font split, weight does the hierarchy work (400 → 500 → 600 → 700).
- Flat, bordered surfaces; soft `shadow-theme-*` reserved strictly for floating overlays.
- Full-parity dark mode on a cool indigo-gray, delivered through Tailwind's `dark:` variant on every component.
- Layout rhythm: fixed sidebar (290/90px) + top header → filter bar → responsive grid of KPI cards, charts, and paginated tables.

## Colors

> Design tokens live in `src/index.css` under the Tailwind v4 `@theme` block (CSS-first config; there is no `tailwind.config.js`). Every colour is a 25→950 twelve-step scale in the Untitled UI system; the **500** step is the interaction anchor for each family.

### Brand & Accent
- **Brand Blue** (`{colors.primary}` — #465fff, `brand-500`): The iconic colour. Primary button fill, active nav item text, focus rings, selected filters, first chart series. Hover deepens to `{colors.primary-hover}` (#3641f5, `brand-600`). The single most-used action colour.
- **Brand Soft** (`{colors.primary-soft}` — #ecf3ff, `brand-50`): Tint fill behind the active nav item and selected states, paired with brand-blue text. In dark mode this becomes `brand-500/[0.12]` with `brand-400` text.
- **Accent Purple** (`{colors.accent-purple}` — #7a5af8) & **Accent Pink** (`{colors.accent-pink}` — #ee46bc): Reserved chart-series colours only; they never appear in chrome.

### Semantic (data-state only)
- **Success Green** (`{colors.success}` — #12b76a): Positive KPI deltas, success alerts, healthy status.
- **Error Red** (`{colors.error}` — #f04438): Negative deltas, destructive actions, error alerts.
- **Warning Amber** (`{colors.warning}` — #f79009): Warnings and attention states.
- **Info Cyan** (`{colors.info}` — #0ba5ec, `blue-light-500`): Informational badges and highlights.

### Surface
- **Canvas** (`{colors.canvas}` — #f9fafb, `gray-50`): The near-white page base behind all cards.
- **Surface** (`{colors.surface}` — #ffffff): White card / panel / sidebar / header fill.
- **Surface Dark** (`{colors.surface-dark}` — #1a2231, `gray-dark`) & **Canvas Dark** (`{colors.canvas-dark}` — #101828): The dark-mode counterparts; cards render as a `white/[0.03]` wash over these.

### Text
- **Ink** (`{colors.ink}` — #1d2939, `gray-800`): Primary body and heading text. Dark mode → `white/90`.
- **Ink Strong** (`{colors.ink-strong}` — #101828, `gray-900`): KPI values and the heaviest headings.
- **Muted Ink** (`{colors.ink-muted}` — #667085, `gray-500`): Secondary text, labels, table headers. Dark mode → `gray-400`.
- **Subtle Ink** (`{colors.ink-subtle}` — #98a2b3, `gray-400`): Placeholders, disabled text, captions.

### Hairlines
- **Hairline** (`{colors.hairline}` — #e4e7ec, `gray-200`): The default 1px border on every card, table row, and divider. Tailwind v4's base layer resets the global border colour to this value. Dark mode → `gray-800`.
- **Hairline Strong** (`{colors.hairline-strong}` — #d0d5dd, `gray-300`): Form-control borders and stronger dividers.

## Typography

### Font Family
- **Outfit** — the one and only typeface, loaded from Google Fonts and applied globally on `body` (`font-outfit`). A clean geometric sans that stays neutral and legible at data-table density. There is no display/body font split; hierarchy is carried entirely by size and weight.
- **Monospace stack** (`ui-monospace, SFMono-Regular, Menlo, Consolas`) — used only for inline `code` inside the AI-assistant Markdown renderer and the SQL editor.

**Note on substitutes:** Outfit is open-source (SIL OFL) — no substitution needed. If unavailable, any neutral geometric grotesque (Inter, Plus Jakarta Sans) preserves the analytical tone. Keep weight, not font-switching, as the hierarchy mechanism.

### Hierarchy

| Token | Size | Weight | Line Height | Use |
|---|---|---|---|---|
| `{typography.title-2xl}` | 72px | 700 | 90px | Marketing / hero splash (rare in-app) |
| `{typography.title-lg}` | 48px | 700 | 60px | Large page hero |
| `{typography.title-md}` | 36px | 700 | 44px | Auth-page headline |
| `{typography.title-sm}` | 30px | 600 | 38px | Section / page title |
| `{typography.kpi-value}` | 22px | 700 | 28px | KPI card big number |
| `{typography.theme-xl}` | 20px | 600 | 30px | Card title, block heading |
| `{typography.body}` | 16px | 400 | 1.5 | Default body copy |
| `{typography.theme-sm}` | 14px | 500 | 20px | Nav items, buttons, table cells, form labels — the workhorse size |
| `{typography.theme-xs}` | 12px | 500 | 18px | Badges, table headers, captions, chart tooltips |

### Principles
- **Weight, not font, builds hierarchy.** Outfit runs the full ladder: `font-normal` (400) body, `font-medium` (500) UI labels and nav, `font-semibold` (600) card titles, `font-bold` (700) KPI values and headings.
- The everyday interface lives at **14px** (`text-theme-sm`) — nav, buttons, table rows, form labels — with 12px (`text-theme-xs`) for metadata. The large `title-*` sizes are for auth/hero moments, not dashboard chrome.
- Headings are calm and sentence-like, never all-caps or tracked. The tone is informational, not promotional.

## Layout

### Spacing System
- **Base unit**: 4px.
- **Tokens**: `{spacing.xxs}` 4px · `{spacing.xs}` 8px · `{spacing.sm}` 12px · `{spacing.md}` 16px · `{spacing.lg}` 20px · `{spacing.xl}` 24px · `{spacing.xxl}` 32px · `{spacing.section}` 40px.
- Card headers pad `{spacing.lg} {spacing.xl}` (px-6 py-5); card bodies `{spacing.md}`–`{spacing.xl}`; buttons `{spacing.sm}` vertical × `{spacing.lg}` horizontal.

### App Shell
- **Fixed sidebar + top header** shell (`AppLayout`). The sidebar (`AppSidebar`) is **290px expanded / 90px collapsed**, with a hover-to-peek state; the content area's left margin animates between `lg:ml-[290px]` and `lg:ml-[90px]` in lockstep. On mobile it becomes an overlay drawer with a `Backdrop`.
- **Three-app switcher**: header pills (`AppSwitcher`) toggle between APP (`/app/*`), TikTok Minis (`/tiktok-minis/*`), and 系统管理 (`/admin/*`). Each app's identity, route prefix, and sidebar nav are defined once in `src/config/apps.tsx`.
- **Nav items** use shared `@utility` classes (`menu-item`, `menu-item-active`, etc.): 12px-radius rows, brand-soft fill + brand text when active, `hover:bg-gray-100` otherwise, with `size-6` icons.

### Grid & Container
- Content is a fluid multi-column grid inside the sidebar-offset area — no fixed max-width column; the dashboard uses the full working width.
- BI boards render a **responsive card grid**: KPI cards, charts, and tables placed on a grid (`FixedBoard` + `WidgetRenderer`), each KPI card computed against a fixed **96px row height** for alignment.
- Pages open with an optional **filter bar** (`DashboardFilterBar` — single/multi selects + date-range picker) above the widget grid.

### Whitespace Philosophy
Density with air: tables and KPI grids pack information tightly, but each card is separated by canvas gutters and framed by a hairline so the eye can parse regions. Cards breathe internally with `{spacing.lg}`–`{spacing.xl}` padding.

### Responsive Strategy

#### Breakpoints
The system overrides Tailwind's default ladder with a dense set tuned for dashboards:

| Name | Width | Note |
|---|---|---|
| `2xsm` | 375px | Small phone |
| `xsm` | 425px | Large phone |
| `sm` | 640px | |
| `md` | 768px | |
| `lg` | 1024px | Sidebar switches from overlay to fixed |
| `xl` | 1280px | |
| `2xl` | 1536px | |
| `3xl` | 2000px | Ultra-wide dashboards |

#### Touch Targets
`{components.button-primary}` and `{components.input}` clear ≥44px height (inputs are a fixed `h-11`). Nav rows and a `BottomTabBar` provide mobile-friendly targets on small screens.

#### Collapsing Strategy
Below `lg` the sidebar collapses to a `Backdrop`-covered drawer. Multi-column KPI/chart grids reflow to fewer columns; wide tables scroll horizontally inside their card (`custom-scrollbar`) rather than reflowing.

#### Image / Media Behavior
The product is chart- and table-first; the main "media" is data viz. Game icons render as small rounded avatars (runtime-downloaded, gitignored). Charts scale fluidly within their card and keep the card's radius.

## Elevation & Depth

| Level | Treatment | Use |
|---|---|---|
| 0 — Flat | No shadow; separation by hairline border + canvas gutter | All cards, tables, KPI tiles, filter bars |
| 1 — `shadow-theme-xs` | `0 1px 2px rgba(16,24,40,0.05)` | Primary buttons, inputs |
| 2 — `shadow-theme-sm` | `0 1px 3px + 0 1px 2px rgba(16,24,40,0.1/0.06)` | Toasts, small popovers, chart tooltips |
| 3 — `shadow-theme-md` | `0 4px 8px -2px + 0 2px 4px -2px` | Date picker, elevated panels |
| 4 — `shadow-theme-lg` | `0 12px 16px -4px + 0 4px 6px -2px` | Dropdown menus |
| 5 — `shadow-theme-xl` | `0 20px 24px -4px + 0 8px 8px -4px` | Modals |

The system leans on **hairline borders and the canvas/surface contrast** for structure, and reserves shadow strictly for elements that float above the page. A special `shadow-focus-ring` (`0 0 0 4px rgba(70,95,255,0.12)`) gives focused controls a brand-blue halo. Modals additionally dim the page with a `gray-400/50` overlay under a heavy `backdrop-blur`.

## Shapes

### Border Radius Scale

| Token | Value | Use |
|---|---|---|
| `{rounded.xs}` | 4px | Chart tooltip boxes, tiny chips |
| `{rounded.sm}` | 6px | Compact controls |
| `{rounded.md}` | 8px | Buttons in some contexts, small inputs |
| `{rounded.lg}` | 12px | Primary/outline buttons, inputs, nav items, dropdown items |
| `{rounded.xl}` | 16px | Cards, panels, filter bars, KPI tiles (`rounded-2xl` in Tailwind = the 16px card standard) |
| `{rounded.2xl}` | 24px | Modals (`rounded-3xl`) |
| `{rounded.pill}` | 9999px | Badges, app-switcher pills, avatars, close buttons |
| `{rounded.full}` | 9999px | Circular avatars and icon buttons |

> Naming note: this project's Tailwind classes read `rounded-2xl` for the 16px card standard and `rounded-3xl` for the 24px modal — the token values above are the resolved pixels, not the class numerals.

### Icon Geometry
Icons are line-style SVGs imported as React components (`vite-plugin-svgr`) from `src/icons`, drawn on a 24×24 box with `stroke-width: 1.5` and round caps/joins. Nav icons are forced to `size-6`. Avatars and icon buttons are fully circular.

## Components

> Every component ships both light and dark styling via Tailwind's `dark:` variant. Specs below describe the Default state; hover/active/disabled follow the neutral hover-fill and `opacity-50` disabled conventions.

### Buttons

**`button-primary`** — the brand-blue action button
- Background `{colors.primary}`, text `{colors.on-primary}`, `shadow-theme-xs`, type `{typography.theme-sm}`, rounded `{rounded.lg}`, padding `{spacing.sm} {spacing.lg}` (md size). Hover → `{colors.primary-hover}`; disabled → `brand-300`. The everyday primary action.

**`button-outline`** — neutral secondary button
- Background `{colors.surface}`, text `{colors.ink}`, `ring-1 ring-gray-300`, rounded `{rounded.lg}`, same padding. Hover → `gray-50`. Dark: `gray-800` fill + `gray-700` ring.

### Cards & Containers

**`component-card`** — the universal content card
- Background `{colors.surface}`, `border {colors.hairline}`, rounded `{rounded.xl}`, **no shadow**. Optional header (`px-6 py-5`, title at `{typography.theme-xl}`) over a `p-4 sm:p-6` body. Dark: `white/[0.03]` fill + `gray-800` border. The base container for nearly every panel.

**`kpi-card`** — the metric tile
- White surface, hairline border, rounded `{rounded.xl}`, tuned to a fixed 96px row. Title in 13px `gray-700`, value in `{typography.kpi-value}` (22px/700) coloured by delta rule (`success-600` / `error-600` / `warning-500` / `brand-600`), optional single-line truncated note in `gray-400`.

**`filter-bar`** — dashboard filter row
- Surface card with hairline border, rounded `{rounded.xl}`, holding single/multi-select dropdowns and a date-range picker. Sits above the widget grid.

### Inputs & Forms

**`input`** — text field (`InputField`)
- Fixed `h-11` (44px), `bg-transparent`, `border {colors.hairline-strong}`, rounded `{rounded.lg}`, `shadow-theme-xs`, 14px text, `placeholder:text-gray-400`. Focus → `border-brand-300` + `ring-3 ring-brand-500/20`. State variants swap the border/ring to `error-500` or `success-500`; an optional `hint` line below renders in the matching semantic colour. Dark: `gray-900` fill, `gray-700` border.

**Labels & controls** — `Label`, `Checkbox`, `Radio`, `Select`, `MultiSelect`, `TextArea`, `date-picker`, `switch` share the 12px-radius / brand-focus language. Checkboxes and radios fill `brand-500` when checked.

### Navigation

**`sidebar`** — fixed left navigation (`AppSidebar`)
- White surface, hairline right border, 290px/90px width with hover-peek. Renders the active app's nav from `src/config/apps.tsx`. Nav rows use the `menu-item*` utilities; active = brand-soft fill + brand text.

**`header`** — top app bar (`AppHeader`)
- White surface, hairline bottom border, holding the `AppSwitcher` pills, search, notifications, theme toggle, and `UserDropdown`.

**`app-switcher-pill`** — three-app toggle
- Pill-shaped segmented control on the canvas; the active app pill lifts to white with strong-ink text, inactive pills stay muted.

### Signature Components

**`modal`** — dialog surface (`ui/modal`)
- White surface, rounded `{rounded.2xl}` (rounded-3xl), `shadow-theme-xl`, over a `gray-400/50` + `backdrop-blur-[32px]` overlay. Circular gray close button top-right; Escape-to-close; body scroll locked while open. A fullscreen variant drops the chrome for the dataset-editor / modeling routes.

**`dropdown`** — menu / popover
- White surface, hairline border, rounded `{rounded.xl}`, `shadow-theme-lg`, `p-2` with 12px-radius menu rows (`menu-dropdown-item*` utilities). Used for user menu, notifications, and select popovers.

**`data-table`** — paginated list table (`TableWidget`, BI tables)
- Header row on the `{colors.canvas}` fill in muted 12px caps; body rows in 14px ink separated by hairlines. **Server-side pagination** is the standard (`page`/`pageSize`, response `{rows, count, page, pageSize}`) with a `PaginationBar` footer. Wide tables scroll horizontally inside the card.

**`badge`** — status / category chip (`ui/badge`)
- Pill-shaped, `px-2.5 py-0.5`. `light` variant = tinted fill + coloured text (default `brand-50` / `brand-500`); `solid` = filled + white text. Colours: primary / success / error / warning / info / light / dark. Sizes sm (12px) / md (14px).

**`alert-strip`** — inline notice (`AlertStripWidget`, `ui/alert`)
- Surface card with a semantic left accent and matching icon; used for dashboard notices and form feedback.

### Charts (`components/bi/dashboard/ChartWidget`, `components/charts`)

The BI charts are ApexCharts wrappers globally re-skinned in `index.css` to match the token system:
- **Categorical palette** (donut / pie, `PIE_PALETTE`), fixed order: `#465fff` (brand) · `#12b76a` · `#f79009` · `#f04438` · `#7a5af8` · `#0ba5ec` · `#fd853a` · `#36bffa` · `#ee46bc` · `#667085` (gray, reserved for the "Other" bucket). Donuts cap at 9 slices + Other; detail expands to Top 5.
- **Single-series** charts default to brand blue `#465fff`; **compare** (current vs prior) uses `['#465fff', '#98a2b3']` (brand + gray).
- Legend/axis text → `gray-700` (dark `gray-400`); gridlines → `gray-100` (dark `gray-800`); tooltips → white rounded card with `shadow-theme-sm`. The donut uses a special `.apex-tooltip-plain` box: white fill, `#d0d5dd` 1px border, plain black 12px text, no shadow.

### Examples (illustrative)

> Kit-mirror demonstration surfaces mapped onto this project's real primitives so downstream consumers re-skin the same 10 surfaces consistently. Each `ex-*` re-uses `component-card`, `input`, `button`, and `badge` chrome.

**`ex-pricing-tier`** — Default tier card. Component-card chrome on white surface.
- Properties: `backgroundColor`, `textColor`, `borderColor`, `rounded`, `padding`

**`ex-pricing-tier-featured`** — Featured tier — brand-soft fill + brand border to promote it.
- Properties: `backgroundColor`, `textColor`, `borderColor`, `rounded`, `padding`

**`ex-product-selector`** — Summary / What's Included card — component-card + divided list.
- Properties: `backgroundColor`, `rounded`, `padding`

**`ex-cart-drawer`** — Right-side summary drawer — surface fill, hairline item dividers.
- Properties: `backgroundColor`, `rounded`, `padding`, `item-divider`

**`ex-app-shell-row`** — Sidebar nav row. Active = brand-soft fill + brand text.
- Properties: `backgroundColor`, `activeIndicator`, `rounded`, `padding`

**`ex-data-table-cell`** — Default th + td chrome. Header canvas fill + muted xs caps; body theme-sm.
- Properties: `headerBackground`, `headerTypography`, `bodyTypography`, `cellPadding`, `rowBorder`

**`ex-auth-form-card`** — Sign-in / sign-up card — component-card wrapping input + button primitives.
- Properties: `backgroundColor`, `rounded`, `padding`

**`ex-modal-card`** — Modal dialog surface — 24px radius, xl shadow, blurred gray overlay.
- Properties: `backgroundColor`, `rounded`, `padding`

**`ex-empty-state-card`** — Empty-state frame — component-card, centered muted caption.
- Properties: `backgroundColor`, `rounded`, `padding`, `captionTypography`

**`ex-toast`** — Toast / alert strip — surface fill, semantic left accent, sm shadow.
- Properties: `backgroundColor`, `rounded`, `padding`, `typography`


## Do's and Don'ts

### Do
- Keep the canvas near-white (`{colors.canvas}`) and structure the page with hairline-bordered white cards.
- Reserve brand blue (`{colors.primary}`) for the primary action and the selected/active state — one blue thing per region.
- Use the semantic palette (green/red/amber/cyan) only to encode data state (deltas, status, alerts), never for decoration.
- Let weight carry hierarchy: 400 body → 500 UI labels → 600 titles → 700 KPI numbers, all in Outfit.
- Ship both light and dark variants on every component via the `dark:` variant; use `white/[0.03]` card fills in dark mode.
- Keep surfaces flat and bordered; spend shadow only on things that truly float (dropdowns, modals, tooltips).
- Round controls at 12px and cards at 16px; keep modals at 24px.

### Don't
- Don't introduce gradients, full-bleed colour bands, or coloured page backgrounds — the neutral canvas + card system is the brand.
- Don't use brand blue as a general accent or paint multiple blue elements in one region; it dilutes "this is the action."
- Don't repurpose semantic colours (green/red/amber) as brand accents — they must stay meaningful.
- Don't reach for a second display typeface or all-caps tracked headlines; Outfit + weight is the whole voice.
- Don't add drop shadows to resting cards; hairline borders and canvas contrast do the separation.
- Don't hard-code raw hex values in components — add tokens to the `@theme` block and reference the Tailwind utilities.
- Don't skip the dark-mode pass — every new surface needs its `dark:` counterpart, or it breaks the system.
