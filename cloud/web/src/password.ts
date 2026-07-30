const encoder = new TextEncoder()

export function passwordByteLength(value: string): number {
  return encoder.encode(value).byteLength
}

export function isValidPassword(value: string): boolean {
  const length = passwordByteLength(value)
  return length >= 8 && length <= 72
}
