/**
 * 简单 semver 比较：返回 -1 / 0 / 1
 * 支持 major.minor.patch 格式
 */
export function compareSemver(a: string, b: string): number {
  const pa = a.split(".").map(Number);
  const pb = b.split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    const va = pa[i] || 0;
    const vb = pb[i] || 0;
    if (va > vb) return 1;
    if (va < vb) return -1;
  }
  return 0;
}

/** a > b */
export function isNewerVersion(a: string, b: string): boolean {
  return compareSemver(a, b) > 0;
}
