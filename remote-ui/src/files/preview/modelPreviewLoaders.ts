import { extension } from '../fileUtils'
import { modelPreviewFormatLabel } from '../modelFileTypes'
import type { ModelBufferGeometry, ModelMaterial, ModelMesh, ModelObject3D, ModelPoints, ThreeModule } from './modelPreviewTypes'

export async function loadModelPreviewObject(
  three: ThreeModule,
  name: string,
  buffer: ArrayBuffer,
  text: string,
): Promise<{ object: ModelObject3D; label: string }> {
  const ext = extension(name)
  if (ext === 'stl') {
    const module = await import('three/examples/jsm/loaders/STLLoader.js')
    const geometry = new module.STLLoader().parse(buffer)
    return { object: meshFromGeometry(three, geometry), label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'obj') {
    const module = await import('three/examples/jsm/loaders/OBJLoader.js')
    return { object: new module.OBJLoader().parse(text), label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'ply') {
    const module = await import('three/examples/jsm/loaders/PLYLoader.js')
    const geometry = new module.PLYLoader().parse(buffer)
    return { object: meshOrPointsFromGeometry(three, geometry), label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'glb' || ext === 'gltf') {
    const module = await import('three/examples/jsm/loaders/GLTFLoader.js')
    const gltf = await new module.GLTFLoader().parseAsync(ext === 'glb' ? buffer : text, '')
    return { object: gltf.scene, label: modelPreviewFormatLabel(name) }
  }
  if (ext === '3mf') {
    const module = await import('three/examples/jsm/loaders/3MFLoader.js')
    return { object: new module.ThreeMFLoader().parse(buffer), label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'fbx') {
    const module = await import('three/examples/jsm/loaders/FBXLoader.js')
    return { object: new module.FBXLoader().parse(buffer, ''), label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'dae') {
    const module = await import('three/examples/jsm/loaders/ColladaLoader.js')
    const collada = new module.ColladaLoader().parse(text, '')
    if (!collada?.scene) throw new Error('Collada file did not contain a scene.')
    return { object: collada.scene, label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'wrl' || ext === 'vrml') {
    const module = await import('three/examples/jsm/loaders/VRMLLoader.js')
    return { object: new module.VRMLLoader().parse(text, ''), label: modelPreviewFormatLabel(name) }
  }
  if (ext === '3ds') {
    const module = await import('three/examples/jsm/loaders/TDSLoader.js')
    return { object: new module.TDSLoader().parse(buffer, ''), label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'amf') {
    const module = await import('three/examples/jsm/loaders/AMFLoader.js')
    return { object: new module.AMFLoader().parse(buffer), label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'pcd') {
    const module = await import('three/examples/jsm/loaders/PCDLoader.js')
    return { object: new module.PCDLoader().parse(buffer), label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'vtk') {
    const module = await import('three/examples/jsm/loaders/VTKLoader.js')
    const geometry = new module.VTKLoader().parse(buffer, '')
    return { object: meshFromGeometry(three, geometry), label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'xyz') {
    const module = await import('three/examples/jsm/loaders/XYZLoader.js')
    const geometry = new module.XYZLoader().parse(text, () => {}) as ModelBufferGeometry
    return { object: pointsFromGeometry(three, geometry), label: modelPreviewFormatLabel(name) }
  }
  if (ext === 'vox') {
    const module = await import('three/examples/jsm/loaders/VOXLoader.js')
    const result = new module.VOXLoader().parse(buffer)
    if (result.scene) return { object: result.scene, label: modelPreviewFormatLabel(name) }
    const group = new three.Group()
    for (const chunk of result.chunks) group.add(module.buildMesh(chunk))
    return { object: group, label: modelPreviewFormatLabel(name) }
  }
  throw new Error('This 3D model format is not supported yet.')
}

export function normalizeModelObject(three: ThreeModule, object: ModelObject3D): ModelObject3D {
  const box = new three.Box3().setFromObject(object)
  if (box.isEmpty()) return object
  const center = new three.Vector3()
  const size = new three.Vector3()
  box.getCenter(center)
  box.getSize(size)
  const maxAxis = Math.max(size.x, size.y, size.z)
  if (!Number.isFinite(maxAxis) || maxAxis <= 0) return object

  const group = new three.Group()
  group.name = object.name || 'Model preview root'
  group.add(object)
  object.position.sub(center)
  if (Number.isFinite(maxAxis) && maxAxis > 0) {
    const scale = 2 / maxAxis
    group.scale.setScalar(scale)
  }
  group.updateMatrixWorld(true)
  return group
}

export function disposeModelObject(object: ModelObject3D | null): void {
  if (!object) return
  object.traverse((child) => {
    const mesh = child as Partial<ModelMesh> & Partial<ModelPoints>
    const geometry = mesh.geometry as ModelBufferGeometry | undefined
    geometry?.dispose()
    const material = mesh.material as ModelMaterial | ModelMaterial[] | undefined
    if (Array.isArray(material)) {
      material.forEach((item) => item.dispose())
      return
    }
    material?.dispose()
  })
}

function meshFromGeometry(three: ThreeModule, geometry: ModelBufferGeometry): ModelMesh {
  geometry.computeVertexNormals()
  geometry.computeBoundingSphere()
  return new three.Mesh(geometry, new three.MeshStandardMaterial({
    color: 0x9fd5ff,
    metalness: 0.12,
    roughness: 0.48,
    side: three.DoubleSide,
  }))
}

function meshOrPointsFromGeometry(three: ThreeModule, geometry: ModelBufferGeometry): ModelObject3D {
  if (geometry.index && geometry.getAttribute('position')?.count >= 3) return meshFromGeometry(three, geometry)
  return pointsFromGeometry(three, geometry)
}

function pointsFromGeometry(three: ThreeModule, geometry: ModelBufferGeometry): ModelPoints {
  const vertexColors = geometry.hasAttribute('color')
  return new three.Points(geometry, new three.PointsMaterial({
    color: vertexColors ? 0xffffff : 0x9fd5ff,
    size: 0.025,
    vertexColors,
  }))
}
