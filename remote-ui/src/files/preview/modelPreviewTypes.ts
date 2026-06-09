export type ThreeModule = typeof import('three')
export type ModelObject3D = ThreeModule['Object3D']['prototype']
export type ModelBufferGeometry = ThreeModule['BufferGeometry']['prototype']
export type ModelMaterial = ThreeModule['Material']['prototype']
export type ModelMesh = ThreeModule['Mesh']['prototype']
export type ModelPoints = ThreeModule['Points']['prototype']
export type ModelWebGLRenderer = ThreeModule['WebGLRenderer']['prototype']

export interface ModelQuaternionState {
  x: number
  y: number
  z: number
  w: number
}

export interface ModelViewState {
  distance: number
  rotation: ModelQuaternionState
}
