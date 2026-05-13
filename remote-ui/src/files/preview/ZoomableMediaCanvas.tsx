import { useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode, type WheelEvent as ReactWheelEvent } from 'react'
import { RotateCcw, RotateCw, ZoomIn, ZoomOut } from 'lucide-react'
import { hapticSelection } from '../../platform/haptics'
import { clamp } from '../fileUtils'
import { MediaPreviewShell } from './MediaPreviewShell'

interface ZoomTransform {
  scale: number
  x: number
  y: number
  rotation: number
}

interface ZoomPoint {
  x: number
  y: number
}

const defaultZoomTransform: ZoomTransform = { scale: 1, x: 0, y: 0, rotation: 0 }
const minPreviewScale = 0.25
const maxPreviewScale = 6

export function ZoomableMediaCanvas({
  toolbar,
  zoomLabel,
  children,
}: {
  toolbar?: ReactNode
  zoomLabel: string
  children: ReactNode
}) {
  const viewportRef = useRef<HTMLDivElement>(null)
  const pointersRef = useRef(new Map<number, ZoomPoint>())
  const transformRef = useRef<ZoomTransform>(defaultZoomTransform)
  const gestureRef = useRef<
    | {
      type: 'pan'
      startTransform: ZoomTransform
      startPointer: ZoomPoint
    }
    | {
      type: 'pinch'
      startTransform: ZoomTransform
      startDistance: number
      startCenter: ZoomPoint
    }
    | null
  >(null)
  const [transform, setTransform] = useState<ZoomTransform>(defaultZoomTransform)

  const commitTransform = (next: ZoomTransform) => {
    const clamped = {
      scale: clamp(next.scale, minPreviewScale, maxPreviewScale),
      x: clamp(next.x, -20000, 20000),
      y: clamp(next.y, -20000, 20000),
      rotation: normalizeRotation(next.rotation),
    }
    transformRef.current = clamped
    setTransform(clamped)
  }

  const resetZoom = () => {
    hapticSelection()
    pointersRef.current.clear()
    gestureRef.current = null
    commitTransform(defaultZoomTransform)
  }

  const zoomAt = (point: ZoomPoint, scale: number) => {
    commitTransform(scaleTransformAroundPoint(transformRef.current, point, scale))
  }

  const zoomBy = (factor: number, point = viewportCenter(viewportRef)) => {
    hapticSelection()
    zoomAt(point, transformRef.current.scale * factor)
  }

  const rotateBy = (degrees: number) => {
    hapticSelection()
    commitTransform({
      ...transformRef.current,
      rotation: transformRef.current.rotation + degrees,
    })
  }

  const startGestureFromPointers = () => {
    const pointers = Array.from(pointersRef.current.values())
    if (pointers.length >= 2) {
      const first = pointers[0]
      const second = pointers[1]
      if (!first || !second) return
      gestureRef.current = {
        type: 'pinch',
        startTransform: transformRef.current,
        startDistance: Math.max(1, distance(first, second)),
        startCenter: midpoint(first, second),
      }
      return
    }
    if (pointers.length === 1) {
      const pointer = pointers[0]
      if (!pointer) return
      gestureRef.current = {
        type: 'pan',
        startTransform: transformRef.current,
        startPointer: pointer,
      }
      return
    }
    gestureRef.current = null
  }

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 && event.pointerType === 'mouse') return
    event.currentTarget.setPointerCapture?.(event.pointerId)
    pointersRef.current.set(event.pointerId, relativePointer(event, viewportRef))
    startGestureFromPointers()
  }

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!pointersRef.current.has(event.pointerId)) return
    pointersRef.current.set(event.pointerId, relativePointer(event, viewportRef))
    const gesture = gestureRef.current
    if (!gesture) return
    if (gesture.type === 'pan') {
      const pointer = pointersRef.current.get(event.pointerId)
      if (!pointer) return
      commitTransform({
        scale: gesture.startTransform.scale,
        x: gesture.startTransform.x + pointer.x - gesture.startPointer.x,
        y: gesture.startTransform.y + pointer.y - gesture.startPointer.y,
        rotation: gesture.startTransform.rotation,
      })
      return
    }
    const pointers = Array.from(pointersRef.current.values())
    if (pointers.length < 2) return
    const first = pointers[0]
    const second = pointers[1]
    if (!first || !second) return
    const nextDistance = Math.max(1, distance(first, second))
    const nextCenter = midpoint(first, second)
    const centered = scaleTransformAroundPoint(
      gesture.startTransform,
      gesture.startCenter,
      gesture.startTransform.scale * (nextDistance / gesture.startDistance),
    )
    commitTransform({
      scale: centered.scale,
      x: centered.x + nextCenter.x - gesture.startCenter.x,
      y: centered.y + nextCenter.y - gesture.startCenter.y,
      rotation: gesture.startTransform.rotation,
    })
  }

  const onPointerEnd = (event: ReactPointerEvent<HTMLDivElement>) => {
    pointersRef.current.delete(event.pointerId)
    event.currentTarget.releasePointerCapture?.(event.pointerId)
    startGestureFromPointers()
  }

  const onWheel = (event: ReactWheelEvent<HTMLDivElement>) => {
    event.preventDefault()
    const point = relativeWheelPoint(event, viewportRef)
    const factor = Math.exp(-event.deltaY * 0.0015)
    zoomAt(point, transformRef.current.scale * factor)
  }

  const onDoubleClick = (event: ReactPointerEvent<HTMLDivElement>) => {
    hapticSelection()
    const point = relativePointer(event, viewportRef)
    if (transformRef.current.scale <= 1.05) {
      zoomAt(point, 2)
      return
    }
    commitTransform(defaultZoomTransform)
  }

  return (
    <MediaPreviewShell toolbar={toolbar}>
      <div
        ref={viewportRef}
        className="relative h-full min-h-[calc(100dvh-7.5rem)] touch-none overflow-hidden bg-zinc-950"
        data-testid="termx-media-zoom-canvas"
        onDoubleClick={onDoubleClick}
        onPointerCancel={onPointerEnd}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerEnd}
        onWheel={onWheel}
      >
        <div className="absolute right-3 top-3 z-20 flex items-center overflow-hidden rounded-lg border border-white/10 bg-black/70 text-white shadow-lg backdrop-blur">
          <button
            type="button"
            aria-label={`Zoom out ${zoomLabel}`}
            title="Zoom out"
            className="flex h-9 w-9 items-center justify-center text-zinc-200 transition-colors active:scale-95 active:bg-white/10"
            onClick={() => zoomBy(0.8)}
          >
            <ZoomOut className="h-4 w-4" />
          </button>
          <span className="min-w-14 border-x border-white/10 px-2 text-center text-[12px] font-semibold tabular-nums text-zinc-200">
            {Math.round(transform.scale * 100)}%
          </span>
          <button
            type="button"
            aria-label={`Zoom in ${zoomLabel}`}
            title="Zoom in"
            className="flex h-9 w-9 items-center justify-center text-zinc-200 transition-colors active:scale-95 active:bg-white/10"
            onClick={() => zoomBy(1.25)}
          >
            <ZoomIn className="h-4 w-4" />
          </button>
          <button
            type="button"
            aria-label={`Reset zoom ${zoomLabel}`}
            title="Reset zoom"
            className="flex h-9 w-9 items-center justify-center border-l border-white/10 text-zinc-200 transition-colors active:scale-95 active:bg-white/10"
            onClick={resetZoom}
          >
            <RotateCcw className="h-4 w-4" />
          </button>
          <button
            type="button"
            aria-label={`Rotate ${zoomLabel}`}
            title="Rotate"
            className="flex h-9 w-9 items-center justify-center border-l border-white/10 text-zinc-200 transition-colors active:scale-95 active:bg-white/10"
            onClick={() => rotateBy(90)}
          >
            <RotateCw className="h-4 w-4" />
          </button>
        </div>
        <div className="flex h-full min-h-[calc(100dvh-7.5rem)] items-center justify-center p-2">
          <div
            className="will-change-transform"
            data-testid="termx-media-transform"
            style={{
              transform: `translate3d(${transform.x}px, ${transform.y}px, 0) rotate(${transform.rotation}deg) scale(${transform.scale})`,
              transformOrigin: 'center',
            }}
          >
            {children}
          </div>
        </div>
      </div>
    </MediaPreviewShell>
  )
}

function scaleTransformAroundPoint(transform: ZoomTransform, point: ZoomPoint, nextScale: number): ZoomTransform {
  const scale = clamp(nextScale, minPreviewScale, maxPreviewScale)
  const ratio = scale / transform.scale
  return {
    scale,
    x: point.x - (point.x - transform.x) * ratio,
    y: point.y - (point.y - transform.y) * ratio,
    rotation: transform.rotation,
  }
}

function relativePointer(
  event: ReactPointerEvent<HTMLDivElement> | ReactMouseEvent<HTMLDivElement>,
  ref: React.RefObject<HTMLDivElement | null>,
): ZoomPoint {
  const rect = ref.current?.getBoundingClientRect()
  if (!rect) return { x: event.clientX, y: event.clientY }
  return { x: event.clientX - rect.left, y: event.clientY - rect.top }
}

function relativeWheelPoint(event: ReactWheelEvent<HTMLDivElement>, ref: React.RefObject<HTMLDivElement | null>): ZoomPoint {
  const rect = ref.current?.getBoundingClientRect()
  if (!rect) return { x: event.clientX, y: event.clientY }
  return { x: event.clientX - rect.left, y: event.clientY - rect.top }
}

function viewportCenter(ref: React.RefObject<HTMLDivElement | null>): ZoomPoint {
  const rect = ref.current?.getBoundingClientRect()
  if (!rect) return { x: 0, y: 0 }
  return { x: rect.width / 2, y: rect.height / 2 }
}

function distance(first: ZoomPoint, second: ZoomPoint): number {
  return Math.hypot(first.x - second.x, first.y - second.y)
}

function midpoint(first: ZoomPoint, second: ZoomPoint): ZoomPoint {
  return {
    x: (first.x + second.x) / 2,
    y: (first.y + second.y) / 2,
  }
}

function normalizeRotation(value: number): number {
  const normalized = value % 360
  return normalized < 0 ? normalized + 360 : normalized
}
