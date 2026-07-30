import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type WheelEvent as ReactWheelEvent } from 'react'
import { AlertCircle, Box, Crosshair, ZoomIn, ZoomOut } from 'lucide-react'
import { hapticSelection } from '../../platform/haptics'
import { useTranslation } from 'react-i18next'
import '../../i18n'
import type { TFunction } from 'i18next'
import { clamp } from '../fileUtils'
import { MediaPreviewShell } from './MediaPreviewShell'
import type { ModelObject3D, ModelQuaternionState, ModelViewState, ModelWebGLRenderer, ThreeModule } from './modelPreviewTypes'

const DEFAULT_MODEL_VIEW: ModelViewState = {
  distance: 1,
  rotation: { x: 0, y: 0, z: 0, w: 1 },
}

export function ModelScene({ object, name, label, three }: { object: ModelObject3D; name: string; label: string; three: ThreeModule }) {
  const { t } = useTranslation()
  const mountRef = useRef<HTMLDivElement>(null)
  const sceneRef = useRef<ModelSceneHandle | null>(null)
  const pointersRef = useRef(new Map<number, ModelPointerPoint>())
  const gestureRef = useRef<ModelGestureState | null>(null)
  const viewRef = useRef<ModelViewState>(DEFAULT_MODEL_VIEW)
  const [view, setView] = useState<ModelViewState>(DEFAULT_MODEL_VIEW)
  const [renderError, setRenderError] = useState<string | null>(null)

  useEffect(() => {
    const mount = mountRef.current
    if (!mount) return undefined

    const initialized = createModelScene(three, mount, object, view, t)
    if (!initialized.ok) {
      sceneRef.current = null
      setRenderError(initialized.message)
      return undefined
    }
    setRenderError(null)
    sceneRef.current = initialized.scene
    initialized.scene.render(view)

    return () => {
      sceneRef.current = null
      initialized.scene.dispose()
    }
  }, [object, t, three])

  useEffect(() => {
    sceneRef.current?.render(view)
  }, [view])

  const commitView = (next: ModelViewState) => {
    const normalized = {
      distance: clamp(next.distance, MODEL_MIN_DISTANCE, MODEL_MAX_DISTANCE),
      rotation: normalizeQuaternion(next.rotation),
    }
    viewRef.current = normalized
    setView(normalized)
  }

  const resetView = () => {
    hapticSelection()
    pointersRef.current.clear()
    gestureRef.current = null
    commitView(DEFAULT_MODEL_VIEW)
  }

  const startRotateGesture = (pointerId: number, point: ModelPointerPoint) => {
    gestureRef.current = {
      kind: 'rotate',
      pointerId,
      point,
      view: viewRef.current,
    }
  }

  const startPinchGesture = () => {
    const pointers = Array.from(pointersRef.current.values())
    if (pointers.length < 2) return
    const first = pointers[0]
    const second = pointers[1]
    if (!first || !second) return
    gestureRef.current = {
      kind: 'pinch',
      distance: Math.max(1, distanceBetweenPoints(first, second)),
      angle: angleBetweenPoints(first, second),
      view: viewRef.current,
    }
  }

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 && event.pointerType === 'mouse') return
    event.currentTarget.setPointerCapture?.(event.pointerId)
    const point = {
      x: event.clientX,
      y: event.clientY,
    }
    pointersRef.current.set(event.pointerId, point)
    if (pointersRef.current.size >= 2) startPinchGesture()
    else startRotateGesture(event.pointerId, point)
  }

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!pointersRef.current.has(event.pointerId)) return
    const point = {
      x: event.clientX,
      y: event.clientY,
    }
    pointersRef.current.set(event.pointerId, point)
    if (pointersRef.current.size >= 2) {
      const gesture = gestureRef.current
      if (!gesture || gesture.kind !== 'pinch') {
        startPinchGesture()
        return
      }
      const pointers = Array.from(pointersRef.current.values())
      const first = pointers[0]
      const second = pointers[1]
      if (!first || !second) return
      const currentDistance = Math.max(1, distanceBetweenPoints(first, second))
      const currentAngle = angleBetweenPoints(first, second)
      commitView({
        ...gesture.view,
        distance: gesture.view.distance * (gesture.distance / currentDistance),
        rotation: rotateQuaternionAroundAxis(gesture.view.rotation, { x: 0, y: 0, z: 1 }, currentAngle - gesture.angle),
      })
      return
    }
    const gesture = gestureRef.current
    if (!gesture || gesture.kind !== 'rotate' || gesture.pointerId !== event.pointerId) {
      startRotateGesture(event.pointerId, point)
      return
    }
    const deltaX = point.x - gesture.point.x
    const deltaY = point.y - gesture.point.y
    commitView({
      distance: gesture.view.distance,
      rotation: dragRotation(gesture.view.rotation, deltaX, deltaY),
    })
  }

  const onPointerEnd = (event: ReactPointerEvent<HTMLDivElement>) => {
    pointersRef.current.delete(event.pointerId)
    event.currentTarget.releasePointerCapture?.(event.pointerId)
    if (pointersRef.current.size >= 2) {
      startPinchGesture()
      return
    }
    if (pointersRef.current.size === 1) {
      const remaining = Array.from(pointersRef.current.entries())[0]
      if (remaining) startRotateGesture(remaining[0], remaining[1])
      return
    }
    gestureRef.current = null
  }

  const onWheel = (event: ReactWheelEvent<HTMLDivElement>) => {
    event.preventDefault()
    const factor = Math.exp(event.deltaY * 0.001)
    commitView({ ...view, distance: view.distance * factor })
  }

  return (
    <MediaPreviewShell
      toolbar={(
        <div className="sticky top-0 z-20 flex min-h-11 items-center justify-between gap-2 border-b border-white/10 bg-black/85 px-4 py-2 text-zinc-200 backdrop-blur">
          <div className="flex min-w-0 items-center gap-2">
            <Box className="h-4 w-4 shrink-0 text-sky-300" />
            <span className="truncate text-[12px] font-semibold uppercase tracking-wide text-zinc-300">{label}</span>
          </div>
          <div className="flex items-center overflow-hidden border border-white/10 bg-white/5">
            <button
              type="button"
              aria-label={t('files.preview.zoomOutNamed', { name })}
              title={t('files.preview.zoomOut')}
              className="flex h-8 w-8 items-center justify-center text-zinc-200 transition-colors active:scale-95 hover:bg-white/5 active:bg-white/10"
              onClick={() => {
                hapticSelection()
                commitView({ ...view, distance: view.distance * 1.2 })
              }}
            >
              <ZoomOut className="h-4 w-4" />
            </button>
            <button
              type="button"
              aria-label={t('files.preview.zoomInNamed', { name })}
              title={t('files.preview.zoomIn')}
              className="flex h-8 w-8 items-center justify-center border-l border-white/10 text-zinc-200 transition-colors active:scale-95 hover:bg-white/5 active:bg-white/10"
              onClick={() => {
                hapticSelection()
                commitView({ ...view, distance: view.distance / 1.2 })
              }}
            >
              <ZoomIn className="h-4 w-4" />
            </button>
            <button
              type="button"
              aria-label={t('files.preview.resetViewNamed', { name })}
              title={t('files.preview.resetView')}
              className="flex h-8 w-8 items-center justify-center border-l border-white/10 text-zinc-200 transition-colors active:scale-95 hover:bg-white/5 active:bg-white/10"
              onClick={resetView}
            >
              <Crosshair className="h-4 w-4" />
            </button>
          </div>
        </div>
      )}
    >
      <div
        className="relative h-full min-h-[calc(100dvh-7.5rem)] touch-none overflow-hidden bg-zinc-950"
        onPointerCancel={onPointerEnd}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerEnd}
        onWheel={onWheel}
      >
        <div ref={mountRef} className="absolute inset-0" data-testid="anytty-stl-preview" />
        {renderError ? (
          <div className="absolute inset-0 flex items-center justify-center bg-zinc-950/95 px-6 text-center">
            <div>
              <AlertCircle className="mx-auto h-7 w-7 text-zinc-500" />
              <h3 className="mt-3 text-[16px] font-bold text-zinc-100">{t('files.preview.modelUnavailable')}</h3>
              <p className="mt-2 max-w-sm text-[14px] leading-6 text-zinc-400">{renderError}</p>
            </div>
          </div>
        ) : null}
      </div>
    </MediaPreviewShell>
  )
}

interface ModelPointerPoint {
  x: number
  y: number
}

type ModelGestureState =
  | {
      kind: 'rotate'
      pointerId: number
      point: ModelPointerPoint
      view: ModelViewState
    }
  | {
      kind: 'pinch'
      distance: number
      angle: number
      view: ModelViewState
    }

interface ModelSceneHandle {
  render(view: ModelViewState): void
  dispose(): void
}

const MODEL_MIN_DISTANCE = 0.02
const MODEL_MAX_DISTANCE = 80

function distanceBetweenPoints(first: ModelPointerPoint, second: ModelPointerPoint): number {
  return Math.hypot(second.x - first.x, second.y - first.y)
}

function angleBetweenPoints(first: ModelPointerPoint, second: ModelPointerPoint): number {
  return Math.atan2(second.y - first.y, second.x - first.x)
}

function createModelScene(
  three: ThreeModule,
  mount: HTMLDivElement,
  object: ModelObject3D,
  initialView: ModelViewState,
  t: TFunction,
): { ok: true; scene: ModelSceneHandle } | { ok: false; message: string } {
  const width = Math.max(1, mount.clientWidth)
  const height = Math.max(1, mount.clientHeight)
  if (typeof navigator !== 'undefined' && navigator.userAgent.includes('jsdom')) {
    return { ok: false, message: t('files.preview.webglUnavailable') }
  }
  const canvas = document.createElement('canvas')
  const contextAttributes: WebGLContextAttributes = {
    alpha: false,
    antialias: true,
    preserveDrawingBuffer: true,
  }
  const webglContext = canvas.getContext('webgl2', contextAttributes) ?? canvas.getContext('webgl', contextAttributes)
  if (!webglContext) {
    return { ok: false, message: t('files.preview.webglUnavailable') }
  }
  let renderer: ModelWebGLRenderer
  try {
    renderer = new three.WebGLRenderer({
      canvas,
      context: webglContext as WebGLRenderingContext,
      antialias: true,
      alpha: false,
      preserveDrawingBuffer: true,
    })
  } catch {
    return { ok: false, message: t('files.preview.webglUnavailable') }
  }
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.setSize(width, height, false)
  renderer.setClearColor(0x09090b, 1)
  renderer.domElement.setAttribute('aria-label', t('files.preview.modelViewport'))
  renderer.domElement.style.display = 'block'
  renderer.domElement.style.height = '100%'
  renderer.domElement.style.width = '100%'
  mount.appendChild(renderer.domElement)

  const scene = new three.Scene()
  object.updateMatrixWorld(true)
  const bounds = computeModelBounds(three, object)
  scene.add(object)

  const gridSize = Math.max(3.2, bounds.size.x * 1.3, bounds.size.z * 1.3)
  const grid = new three.GridHelper(gridSize, 16, 0x3f3f46, 0x27272a)
  grid.position.set(bounds.center.x, bounds.minY - 0.02, bounds.center.z)
  scene.add(grid)

  const hemisphere = new three.HemisphereLight(0xffffff, 0x111827, 2.6)
  scene.add(hemisphere)
  const key = new three.DirectionalLight(0xffffff, 2.2)
  key.position.set(3, 4, 5)
  scene.add(key)
  const fill = new three.DirectionalLight(0x8ec5ff, 1.1)
  fill.position.set(-4, 2, -3)
  scene.add(fill)

  const camera = new three.PerspectiveCamera(45, width / height, 0.01, 1000)
  let latestView = initialView
  let latestFitDistance = computeFitDistance(camera.fov, width / height, bounds.radius)
  camera.near = Math.max(0.001, latestFitDistance / 1000)
  camera.far = Math.max(1000, latestFitDistance * 1000)
  camera.updateProjectionMatrix()

  const render = (view: ModelViewState) => {
    latestView = view
    const radius = clamp(view.distance, MODEL_MIN_DISTANCE, MODEL_MAX_DISTANCE) * latestFitDistance
    object.quaternion.copy(quaternionStateToThree(three, view.rotation))
    object.updateMatrixWorld(true)
    camera.position.set(bounds.center.x, bounds.center.y, bounds.center.z + radius)
    camera.lookAt(bounds.center)
    renderer.render(scene, camera)
  }

  const resize = () => {
    const nextWidth = Math.max(1, mount.clientWidth)
    const nextHeight = Math.max(1, mount.clientHeight)
    camera.aspect = nextWidth / nextHeight
    latestFitDistance = computeFitDistance(camera.fov, camera.aspect, bounds.radius)
    camera.near = Math.max(0.001, latestFitDistance / 1000)
    camera.far = Math.max(1000, latestFitDistance * 1000)
    camera.updateProjectionMatrix()
    renderer.setSize(nextWidth, nextHeight, false)
    render(latestView)
  }

  let resizeObserver: ResizeObserver | null = null
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(resize)
    resizeObserver.observe(mount)
  }
  window.addEventListener('resize', resize)

  return {
    ok: true,
    scene: {
      render,
      dispose() {
        window.removeEventListener('resize', resize)
        resizeObserver?.disconnect()
        if (renderer.domElement.parentNode === mount) mount.removeChild(renderer.domElement)
        scene.remove(object)
        renderer.dispose()
      },
    },
  }
}

function dragRotation(base: ModelQuaternionState, deltaX: number, deltaY: number): ModelQuaternionState {
  const length = Math.hypot(deltaX, deltaY)
  if (length < 0.01) return base
  const axis = {
    x: deltaY / length,
    y: deltaX / length,
    z: 0,
  }
  return rotateQuaternionAroundAxis(base, axis, length * 0.01)
}

function rotateQuaternionAroundAxis(
  base: ModelQuaternionState,
  axis: { x: number; y: number; z: number },
  angle: number,
): ModelQuaternionState {
  const length = Math.hypot(axis.x, axis.y, axis.z)
  if (length < 0.000001 || Math.abs(angle) < 0.000001) return base
  const half = angle / 2
  const sin = Math.sin(half)
  const delta = {
    x: axis.x / length * sin,
    y: axis.y / length * sin,
    z: axis.z / length * sin,
    w: Math.cos(half),
  }
  return normalizeQuaternion(multiplyQuaternions(delta, base))
}

function multiplyQuaternions(first: ModelQuaternionState, second: ModelQuaternionState): ModelQuaternionState {
  return {
    x: first.w * second.x + first.x * second.w + first.y * second.z - first.z * second.y,
    y: first.w * second.y - first.x * second.z + first.y * second.w + first.z * second.x,
    z: first.w * second.z + first.x * second.y - first.y * second.x + first.z * second.w,
    w: first.w * second.w - first.x * second.x - first.y * second.y - first.z * second.z,
  }
}

function normalizeQuaternion(value: ModelQuaternionState): ModelQuaternionState {
  const length = Math.hypot(value.x, value.y, value.z, value.w)
  if (!Number.isFinite(length) || length < 0.000001) return DEFAULT_MODEL_VIEW.rotation
  return {
    x: value.x / length,
    y: value.y / length,
    z: value.z / length,
    w: value.w / length,
  }
}

function quaternionStateToThree(three: ThreeModule, value: ModelQuaternionState) {
  const normalized = normalizeQuaternion(value)
  return new three.Quaternion(normalized.x, normalized.y, normalized.z, normalized.w)
}

function computeModelBounds(three: ThreeModule, object: ModelObject3D) {
  const box = new three.Box3().setFromObject(object)
  if (box.isEmpty()) {
    return {
      center: new three.Vector3(0, 0, 0),
      size: new three.Vector3(2, 2, 2),
      minY: -1,
      radius: 1.2,
    }
  }
  const center = new three.Vector3()
  const size = new three.Vector3()
  box.getCenter(center)
  box.getSize(size)
  const radius = Math.max(size.length() / 2, 0.5)
  return {
    center,
    size,
    minY: box.min.y,
    radius,
  }
}

function computeFitDistance(verticalFovDegrees: number, aspect: number, radius: number): number {
  const verticalFov = threeMathDegToRad(verticalFovDegrees)
  const horizontalFov = 2 * Math.atan(Math.tan(verticalFov / 2) * Math.max(aspect, 0.01))
  const limitingFov = Math.max(0.01, Math.min(verticalFov, horizontalFov))
  return Math.max(1, (radius * 1.35) / Math.sin(limitingFov / 2))
}

function threeMathDegToRad(value: number): number {
  return value * Math.PI / 180
}
