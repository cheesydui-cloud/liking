// Place a fixed menu so it stays inside the viewport.
// anchor and menu use viewport coordinates. height is the menu's full content height.
export function placeMoreMenu(anchor, menu, viewport) {
  const margin = 8
  const gap = 4
  const width = Math.max(200, Number(menu?.width) || 0)
  const height = Math.max(0, Number(menu?.height) || 0)
  const vw = Number(viewport?.width) || 0
  const vh = Number(viewport?.height) || 0
  let left = anchor.right - width
  if (left < margin) left = margin
  if (left + width > vw - margin) left = Math.max(margin, vw - width - margin)
  const spaceBelow = vh - anchor.bottom - gap - margin
  const spaceAbove = anchor.top - gap - margin
  const openUp = height > spaceBelow && spaceAbove > spaceBelow
  const room = openUp ? spaceAbove : spaceBelow
  const maxHeight = Math.max(96, Math.floor(room))
  if (openUp) {
    return { top: undefined, bottom: vh - anchor.top + gap, left, maxHeight }
  }
  return { top: anchor.bottom + gap, bottom: undefined, left, maxHeight }
}
