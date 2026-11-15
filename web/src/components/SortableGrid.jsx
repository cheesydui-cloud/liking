import { Fragment, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { api } from '../lib/api'
import { mergeKeepOrder, reorderList, targetIndexFromRects } from '../lib/cardOrder'
import { Icon } from './ui'
import { useToast } from './Layout'

export function useCardOrder(kind, onFail) {
  const toast = useToast()
  const hold = useRef({ on: false, gen: 0 })
  const saving = useRef(false)
  const pending = useRef(null)
  const kindRef = useRef(kind)
  const onFailRef = useRef(onFail)
  const toastRef = useRef(toast)
  kindRef.current = kind
  onFailRef.current = onFail
  toastRef.current = toast

  const apiRef = useRef({})
  apiRef.current.generation = () => hold.current.gen
  apiRef.current.applyIncoming = (prev, incoming, startedGen) => {
    if (hold.current.on || startedGen !== hold.current.gen) return mergeKeepOrder(prev, incoming)
    return incoming
  }
  apiRef.current.hold = () => {
    if (!hold.current.on) {
      hold.current.on = true
      hold.current.gen += 1
    }
  }
  apiRef.current.commit = async (visible) => {
    const ids = (visible || []).map(item => Number(item.id)).filter(id => Number.isFinite(id) && id > 0)
    if (!ids.length) return true
    pending.current = ids
    if (!hold.current.on) {
      hold.current.on = true
      hold.current.gen += 1
    }
    if (saving.current) return true
    saving.current = true
    let failed = false
    try {
      while (pending.current) {
        const batch = pending.current
        pending.current = null
        await api.put('/card-order', { kind: kindRef.current, ids: batch })
      }
    } catch (e) {
      pending.current = null
      failed = true
      toastRef.current(e.message || '顺序没保存', 'error')
    } finally {
      saving.current = false
      hold.current.on = false
      hold.current.gen += 1
    }
    if (failed) onFailRef.current?.()
    return !failed
  }

  const stable = useRef(null)
  if (!stable.current) {
    stable.current = {
      generation: () => apiRef.current.generation(),
      applyIncoming: (...args) => apiRef.current.applyIncoming(...args),
      hold: () => apiRef.current.hold(),
      commit: (...args) => apiRef.current.commit(...args),
    }
  }
  return stable.current
}

export function DragHandle({ drag }) {
  return (
    <button
      type="button"
      className="icon-btn drag-handle"
      aria-label="拖动排序"
      title="拖到任意位置，左右方向键也可以"
      {...drag.handleProps}
    >
      <Icon name="grip" size={14} />
    </button>
  )
}

function interactiveTarget(target) {
  if (!(target instanceof Element)) return false
  const hit = target.closest('button, a, input, textarea, select, label')
  if (!hit) return false
  return !hit.classList.contains('drag-handle')
}

function mountDragFloat(card, e) {
  const rect = card.getBoundingClientRect()
  const clone = card.cloneNode(true)
  clone.classList.add('drag-float')
  clone.classList.remove('is-sorting')
  clone.removeAttribute('data-sort-id')
  clone.removeAttribute('id')
  clone.setAttribute('aria-hidden', 'true')
  clone.querySelectorAll('[id]').forEach((el) => el.removeAttribute('id'))
  clone.style.width = `${rect.width}px`
  clone.style.height = `${rect.height}px`
  clone.style.transformOrigin = `${e.clientX - rect.left}px ${e.clientY - rect.top}px`
  clone.style.transform = 'none'
  document.body.appendChild(clone)
  card.classList.add('is-sorting')
  const float = {
    node: clone,
    ox: e.clientX - rect.left,
    oy: e.clientY - rect.top,
    width: rect.width,
    height: rect.height,
    raf: 0,
  }
  clone.style.left = `${rect.left}px`
  clone.style.top = `${rect.top}px`
  const reduce = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  if (reduce) {
    clone.style.transform = ''
  } else {
    float.raf = requestAnimationFrame(() => {
      float.raf = 0
      if (!float.node?.isConnected) return
      float.node.style.transform = ''
    })
  }
  return float
}

function moveDragFloat(float, e) {
  if (!float?.node) return
  float.node.style.left = `${e.clientX - float.ox}px`
  float.node.style.top = `${e.clientY - float.oy}px`
}

function clearDragFloat(drag) {
  const float = drag?.float
  if (float?.raf) cancelAnimationFrame(float.raf)
  float?.node?.remove()
  if (float) float.node = null
  if (drag) drag.float = null
}

function scrollableParent(el) {
  let node = el?.parentElement
  while (node && node !== document.body) {
    const oy = getComputedStyle(node).overflowY
    if ((oy === 'auto' || oy === 'scroll') && node.scrollHeight > node.clientHeight + 2) return node
    node = node.parentElement
  }
  return document.scrollingElement
}

function autoScroll(grid, clientY) {
  const scroller = scrollableParent(grid)
  if (!scroller) return false
  const bounds = scroller === document.scrollingElement
    ? { top: 0, bottom: window.innerHeight }
    : scroller.getBoundingClientRect()
  const edge = 72
  if (clientY < bounds.top + edge) {
    scroller.scrollBy(0, -16)
    return true
  }
  if (clientY > bounds.bottom - edge) {
    scroller.scrollBy(0, 16)
    return true
  }
  return false
}

function unbindDrag(drag) {
  const h = drag?.handlers
  if (!h) return
  window.removeEventListener('pointermove', h.move)
  window.removeEventListener('pointerup', h.up)
  window.removeEventListener('pointercancel', h.up)
  window.removeEventListener('touchmove', h.touch)
  drag.handlers = null
}

function clearMotion(grid) {
  if (!grid) return
  for (const el of grid.querySelectorAll('[data-sort-id]')) {
    el.style.transition = ''
    el.style.transform = ''
  }
}

export function SortableGrid({ items, className, renderItem, onPreview, onCommit }) {
  const itemsRef = useRef(items)
  itemsRef.current = items
  const onPreviewRef = useRef(onPreview)
  const onCommitRef = useRef(onCommit)
  onPreviewRef.current = onPreview
  onCommitRef.current = onCommit
  const gridRef = useRef(null)
  const dragRef = useRef(null)
  const flipRef = useRef(null)
  const suppressClick = useRef(false)
  const aliveRef = useRef(true)
  const [dragId, setDragId] = useState(null)

  useEffect(() => () => {
    aliveRef.current = false
    const drag = dragRef.current
    clearTimeout(drag?.holdTimer)
    clearTimeout(drag?.settleTimer)
    if (drag?.scrollRaf) cancelAnimationFrame(drag.scrollRaf)
    unbindDrag(drag)
    clearDragFloat(drag)
  }, [])

  useLayoutEffect(() => {
    const before = flipRef.current
    if (!before) return
    flipRef.current = null
    const grid = gridRef.current
    if (!grid) return
    const reduce = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
    for (const el of grid.querySelectorAll('[data-sort-id]')) {
      const prev = before.get(el.dataset.sortId)
      if (!prev) continue
      el.style.transition = 'none'
      el.style.transform = 'none'
      const next = el.getBoundingClientRect()
      const dx = prev.left - next.left
      const dy = prev.top - next.top
      if (reduce || (Math.abs(dx) < 0.5 && Math.abs(dy) < 0.5)) {
        el.style.transition = ''
        el.style.transform = ''
        continue
      }
      el.style.transform = `translate(${dx}px, ${dy}px)`
      void el.offsetWidth
      el.style.transition = 'transform 280ms cubic-bezier(0.22, 1.15, 0.36, 1)'
      el.style.transform = ''
    }
  })

  const bind = (item) => {
    const id = String(item.id)
    const idsOf = (list) => list.map(x => String(x.id))
    const previewFrom = (orderIds) => {
      const byId = new Map(itemsRef.current.map(x => [String(x.id), x]))
      const next = orderIds.map(key => byId.get(key)).filter(Boolean)
      if (next.length !== orderIds.length) return
      const drag = dragRef.current
      if (drag) drag.latest = next
      const grid = gridRef.current
      if (grid) {
        const map = new Map()
        for (const el of grid.querySelectorAll('[data-sort-id]')) {
          map.set(el.dataset.sortId, el.getBoundingClientRect())
        }
        flipRef.current = map
      }
      onPreviewRef.current?.(next)
    }
    const placeAt = (drag, clientX, clientY) => {
      const grid = gridRef.current
      const float = drag.float
      if (!grid || !float || drag.settling) return
      const x = clientX - float.ox + float.width / 2
      const y = clientY - float.oy + float.height / 2
      const cards = [...grid.querySelectorAll('[data-sort-id]')]
      const from = cards.findIndex(el => el.dataset.sortId === drag.id)
      if (from < 0) return
      const rects = cards.map(el => {
        const r = el.getBoundingClientRect()
        return { left: r.left, top: r.top, width: r.width, height: r.height }
      })
      const to = targetIndexFromRects(rects, x, y, from)
      if (to === from) return
      const nextIds = reorderList(cards.map(el => el.dataset.sortId), from, to)
      const wanted = nextIds.join(',')
      const have = (drag.latest || []).map(x => String(x.id)).join(',')
      if (wanted === have) return
      previewFrom(nextIds)
    }
    const finish = (e) => {
      const drag = dragRef.current
      if (!drag || drag.id !== id || drag.settling) return
      if (e && drag.pointerId !== e.pointerId) return
      drag.settling = true
      clearTimeout(drag.holdTimer)
      if (drag.scrollRaf) cancelAnimationFrame(drag.scrollRaf)
      unbindDrag(drag)
      const changed = drag.active && idsOf(drag.latest || itemsRef.current).join(',') !== drag.origin
      const card = drag.card
      const float = drag.float
      const node = float?.node
      const reduce = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
      const done = () => {
        if (!aliveRef.current || dragRef.current !== drag) return
        if (card) card.style.touchAction = ''
        card?.classList.remove('is-sorting')
        clearDragFloat(drag)
        clearMotion(gridRef.current)
        dragRef.current = null
        setDragId(null)
      }
      if (!drag.active || !node || !card || reduce) {
        done()
      } else {
        card.style.transition = 'none'
        card.style.transform = 'none'
        const rect = card.getBoundingClientRect()
        if (float.raf) cancelAnimationFrame(float.raf)
        node.style.transition = 'left 240ms cubic-bezier(0.22, 1.15, 0.36, 1), top 240ms cubic-bezier(0.22, 1.15, 0.36, 1), width 240ms cubic-bezier(0.22, 1.15, 0.36, 1), height 240ms cubic-bezier(0.22, 1.15, 0.36, 1), transform 240ms cubic-bezier(0.22, 1.15, 0.36, 1)'
        node.style.left = `${rect.left}px`
        node.style.top = `${rect.top}px`
        node.style.width = `${rect.width}px`
        node.style.height = `${rect.height}px`
        node.style.transform = 'none'
        let closed = false
        const close = () => {
          if (closed) return
          closed = true
          node.removeEventListener('transitionend', close)
          done()
        }
        node.addEventListener('transitionend', close)
        drag.settleTimer = setTimeout(close, 280)
      }
      if (!drag.active) return
      suppressClick.current = true
      setTimeout(() => { suppressClick.current = false }, 400)
      if (changed) onCommitRef.current?.(drag.latest || itemsRef.current)
    }
    return {
      dragging: dragId != null && String(dragId) === id,
      itemProps: {
        'data-sort-id': id,
        onPointerDown(e) {
          if (e.button != null && e.button !== 0) return
          if (interactiveTarget(e.target)) return
          if (dragRef.current?.settling) return
          if (e.pointerType !== 'touch') {
            e.preventDefault()
            e.currentTarget.setPointerCapture?.(e.pointerId)
          }
          const drag = {
            id,
            pointerId: e.pointerId,
            x: e.clientX,
            y: e.clientY,
            active: false,
            settling: false,
            touch: e.pointerType === 'touch',
            origin: idsOf(itemsRef.current).join(','),
            latest: itemsRef.current,
          }
          const lift = (ev) => {
            if (drag.active || drag.settling || dragRef.current !== drag) return
            drag.active = true
            drag.card = e.currentTarget
            drag.card.style.touchAction = 'none'
            drag.card.setPointerCapture?.(drag.pointerId)
            drag.float = mountDragFloat(drag.card, ev)
            setDragId(id)
            drag.scrollRaf = requestAnimationFrame(function tick() {
              const current = dragRef.current
              if (!current || current !== drag || current.settling) return
              if (current.lastY != null && autoScroll(gridRef.current, current.lastY)) {
                placeAt(current, current.lastX, current.lastY)
              }
              current.scrollRaf = requestAnimationFrame(tick)
            })
          }
          if (drag.touch) {
            drag.holdTimer = setTimeout(() => lift({ clientX: drag.x, clientY: drag.y }), 280)
          }
          const move = (ev) => {
            if (ev.pointerId !== drag.pointerId || drag.settling) return
            drag.lastX = ev.clientX
            drag.lastY = ev.clientY
            const moved = Math.hypot(ev.clientX - drag.x, ev.clientY - drag.y)
            if (!drag.active) {
              if (drag.touch) {
                if (moved > 8) {
                  clearTimeout(drag.holdTimer)
                  unbindDrag(drag)
                  dragRef.current = null
                }
                return
              }
              if (moved < 6) return
              lift(ev)
            }
            moveDragFloat(drag.float, ev)
            if (drag.active) ev.preventDefault()
            placeAt(drag, ev.clientX, ev.clientY)
          }
          const up = (ev) => finish(ev)
          const touch = (ev) => {
            if (!drag.active || drag.settling) return
            ev.preventDefault()
          }
          drag.handlers = { move, up, touch }
          window.addEventListener('pointermove', move)
          window.addEventListener('pointerup', up)
          window.addEventListener('pointercancel', up)
          window.addEventListener('touchmove', touch, { passive: false })
          dragRef.current = drag
        },
        onPointerUp: finish,
        onPointerCancel: finish,
        onClickCapture(e) {
          if (!suppressClick.current) return
          suppressClick.current = false
          e.preventDefault()
          e.stopPropagation()
        },
      },
      handleProps: {
        onKeyDown(e) {
          if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return
          e.preventDefault()
          const list = itemsRef.current
          const from = list.findIndex(x => String(x.id) === id)
          const to = from + (e.key === 'ArrowRight' ? 1 : -1)
          if (from < 0 || to < 0 || to >= list.length) return
          const next = reorderList(list, from, to)
          itemsRef.current = next
          onPreviewRef.current?.(next)
          onCommitRef.current?.(next)
        },
      },
    }
  }

  return (
    <div ref={gridRef} className={`${className || ''}${dragId != null ? ' is-sorting' : ''}`}>
      {items.map(item => (
        <Fragment key={item.id}>{renderItem(item, bind(item))}</Fragment>
      ))}
    </div>
  )
}
