# liking design system

Source of truth for the panel UI. Identity: **运行图**.

Generated 2026-09-13 from ui-ux-pro-max (Data-Dense + Swiss Modernism 2.0), Anthropic frontend-design, and Vercel web-design-guidelines — then rejected landing/Inter/mint defaults.

Do not re-derive from Inter + navy SaaS, or from white-gray-black with no distinctive spend.

## Brief

- Product: private-line ops console (servers → inbounds → packages → users → traffic)
- Audience: one admin, plus subscribers who only see “my subscription”
- Job: is the machine up, can I change a node, can I open an account, is traffic over quota
- Platform: React + Vite + Tailwind 4, desktop-first, phone secondary
- Light is default. Dark is a night switch, not the brand.
- Chinese UI. 纯白. List-first. One solid black CTA.

## Distinctive spend

1. **3px status spine** on every operational row / machine block: live teal `#0F766E`, fault red `#B91C1C`, offline gray `#C8C8C8`. Not a glowing dot.
2. **Giant dashboard count** (`40–64px`) of online machines + 「在线 / 共 N 台」.
3. True black `#000000`, max **2px** radius, **no drop shadow**.
4. Sidebar active = 3px ink left spine + heavier type. Never a gray pill.
5. Tables, not SaaS cards. Filters are text + underline, not chips.

## Tokens

### Color

| Name | Light | Role |
|---|---|---|
| paper | `#FFFFFF` | canvas |
| ink | `#000000` | text, primary CTA |
| mute | `#5C5C5C` | secondary text |
| rule | `#E8E8E8` | borders |
| live | `#0F766E` | spine + 「在线」 only |
| fault | `#B91C1C` | errors, over-quota |

Warn is `#C2410C`. Dark remaps paper/ink and brightens live/fault; do not invert.

Primary CTA is ink-on-paper (black button), never live-teal.

### Type

- UI: IBM Plex Sans 400/500/600 + PingFang SC / 思源黑体
- Data: IBM Plex Mono + `tabular-nums` on IP, port, version, heartbeat, bytes
- Scale: 11 / 13 / 14 / 16. Only the dashboard 在线 count may be 40–64
- No Inter, no ALL-CAPS, no middle dots in chrome

### Layout

- Sidebar ~200px, header 48px, content left-aligned, tables full width of the pane
- Radius max 2px. No drop shadow
- One solid black CTA per page; sync/install as text; uninstall in 更多
- Phone: primary in bottom bar, 44px, tables scroll-x

### Motion

- 150–200ms opacity / transform
- Honor `prefers-reduced-motion`

## Anti-patterns

- Inter + mint `#ECFDF5` / `#059669` landing kit
- Six KPI cards
- Shadows, 8–12px radius
- Dark Grafana as default
- Cream + serif + terracotta
- Full newspaper broadsheet
- Middle dots `A · B · C`
- Teal as the brand / CTA
