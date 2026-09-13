# liking design system

Source of truth for the panel UI. Generated 2026-09-13 by running:

1. `ui-ux-pro-max` (`--design-system --variance 2 --motion 3 --density 8`, then domain searches)
2. Anthropic `frontend-design`
3. Vercel `web-design-guidelines`

Do not re-derive from Inter + navy SaaS defaults.

## Brief

- Product: private-line ops console (servers → inbounds → packages → users → traffic)
- Audience: one admin, plus subscribers who only see “my subscription”
- Job: is the machine up, can I change a node, can I open an account, is traffic over quota
- Platform: React + Vite + Tailwind 4, desktop-first, phone secondary
- Light is default. Dark is a night switch, not the brand.

## What the skills said, and what we kept

| Skill output | Decision |
|---|---|
| ui-ux-pro-max first pass: Exaggerated Minimalism, Inter, `#0F172A` / `#0369A1`, 12rem type, landing hero | **Reject.** Wrong product. Density 8 + admin console wins over the landing pattern. |
| ui-ux-pro-max product search: Data-Dense + Real-Time Monitoring | **Keep.** |
| ui-ux-pro-max style: Data-Dense Dashboard + Swiss Modernism 2.0 (black/white, 8px grid, one accent) | **Keep.** |
| ui-ux-pro-max type: Inter | **Reject.** Inter is the current AI default. Keep Fira Sans + Fira Code already in the app. |
| frontend-design: one memorable thing; no SaaS KPI card kit; no `#111` fake-black; no middle-dots; no ALL-CAPS eyebrows | **Keep.** The memorable thing is the **carrier / live link** (teal), used only for online/ok. |
| frontend-design: follow the brief when it pins a direction | **Keep 纯白 + 实心黑主按钮.** Teal is not the brand, only the live state. |
| Vercel guidelines: skip link, labels, focus-visible, `…`, tabular-nums, theme-color, 44px touch on phone, `aria-live`, no `transition: all` | **Keep.** Chinese UI does not use Title Case. |

## Tokens

### Color (4–6 named)

| Name | Light | Role |
|---|---|---|
| paper | `#FFFFFF` | canvas, cards |
| ink | `#0A0A0A` | text, primary CTA |
| mute | `#6B6B6B` | secondary text |
| rule | `#E3E3E3` | borders |
| live | `#0F766E` | online / ok only |
| fault | `#B91C1C` | errors, over-quota |

Warn is `#C2410C`. Dark mode remaps paper/ink and brightens live/fault; do not invert.

Primary CTA is ink-on-paper (black button), never live-teal and never sky-blue.

### Type

- UI: Fira Sans 400/500/600/700
- Data: Fira Code + `tabular-nums` on ports, bytes, versions, heartbeats
- Scale: 12 / 13 / 14 / 16 / 20. Body 14px (dense dashboard). Headings `text-wrap: pretty`
- No Inter, no display serif, no all-caps labels

### Layout

- Sidebar 216px, header 48px, content max 1280px, left-aligned
- 8px spacing. Cards: 1px rule, no drop shadow
- One solid primary button per page
- Phone: primary action in the bottom bar; touch targets ≥ 44px

### Motion

- 150–200ms, `transform` / `opacity` / `colors` only
- Honor `prefers-reduced-motion`
- No scroll-reveal, no KPI count-up

## Principles

1. Lists over marketing cards. Status is a rail of numbers, not six identical KPI tiles.
2. Problems first: alerts above counts.
3. State is shape + word: green/teal dot **and** 「在线」.
4. Empty states tell the next action. Setup steps stay until there is a user, not until a server name exists.
5. Dangerous actions live in 「更多」.
6. Copy is Chinese, sentence-like, active. Loading ends with `…`. Errors say how to fix.

## Anti-patterns

- Inter + slate-900 + sky-600
- Cream `#F4F1EA` + terracotta
- Dark OLED monitoring wall as default
- Soft grey shadow under every card
- Middle dots in chrome (`A · B · C`)
- Emoji as navigation icons
- Color as the only status signal
