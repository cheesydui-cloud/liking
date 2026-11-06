import { Fragment, useRef, useState } from 'react'
import { api } from '../lib/api'
import { mergeKeepOrder, reorderList } from '../lib/cardOrder'
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
      title="拖动排序，左右方向键也可以"
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

export function SortableGrid({ items, className, renderItem, onPreview, onCommit }) {
  const itemsRef = useRef(items)
  itemsRef.current = items
  const gridRef = useRef(null)
  const dragRef = useRef(null)
  const suppressClick = useRef(false)
  const [dragId, setDragId] = useState(null)

  const bind = (item) => {
    const id = String(item.id)
    const idsOf = (list) => list.map(x => String(x.id))
    const previewFrom = (orderIds) => {
      const byId = new Map(itemsRef.current.map(x => [String(x.id), x]))
      const next = orderIds.map(key => byId.get(key)).filter(Boolean)
      if (next.length !== orderIds.length) return
      const drag = dragRef.current
      if (drag) drag.latest = next
      onPreview?.(next)
    }
    const finish = (e) => {
      const drag = dragRef.current
      if (!drag || drag.id !== id || (e && drag.pointerId !== e.pointerId)) return
      dragRef.current = null
      setDragId(null)
      if (!drag.active) return
      suppressClick.current = true
      setTimeout(() => { suppressClick.current = false }, 400)
      const finalItems = drag.latest || itemsRef.current
      if (idsOf(finalItems).join(',') !== drag.origin) onCommit?.(finalItems)
    }
    return {
      dragging: dragId != null && String(dragId) === id,
      itemProps: {
        'data-sort-id': id,
        onPointerDown(e) {
          if (e.button != null && e.button !== 0) return
          if (interactiveTarget(e.target)) return
          e.currentTarget.setPointerCapture?.(e.pointerId)
          dragRef.current = {
            id,
            pointerId: e.pointerId,
            x: e.clientX,
            y: e.clientY,
            active: false,
            origin: idsOf(itemsRef.current).join(','),
            latest: itemsRef.current,
          }
        },
        onPointerMove(e) {
          const drag = dragRef.current
          if (!drag || drag.id !== id || drag.pointerId !== e.pointerId) return
          if (!drag.active) {
            if (Math.hypot(e.clientX - drag.x, e.clientY - drag.y) < 6) return
            drag.active = true
            setDragId(id)
          }
          e.preventDefault()
          const edge = 56
          if (e.clientY < edge) window.scrollBy(0, -14)
          else if (e.clientY > window.innerHeight - edge) window.scrollBy(0, 14)
          const grid = gridRef.current
          if (!grid) return
          const cards = [...grid.querySelectorAll('[data-sort-id]')]
          let over = null
          for (const el of cards) {
            if (el.dataset.sortId === drag.id) continue
            const rect = el.getBoundingClientRect()
            if (e.clientX >= rect.left && e.clientX <= rect.right && e.clientY >= rect.top && e.clientY <= rect.bottom) {
              over = el.dataset.sortId
              break
            }
          }
          if (!over) return
          const domIds = cards.map(el => el.dataset.sortId)
          const from = domIds.indexOf(drag.id)
          const to = domIds.indexOf(over)
          if (from < 0 || to < 0 || from === to) return
          previewFrom(reorderList(domIds, from, to))
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
          onPreview?.(next)
          onCommit?.(next)
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
