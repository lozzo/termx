# TASK_04 — Frontend TypeScript 简化

**Wave**: 1（无依赖，可与 TASK_01/02/03 同时执行）  
**验证**: `cd remote-ui && npm run build`（或 `npx tsc --noEmit`）→ 无类型错误

---

## 1. 修改 `remote-ui/src/localAppIdentity.ts`

### 删除以下所有内容

- `appPublicKey`、`appCertificate` storage key 常量
- `LocalAppIdentity` interface（含 `appDeviceId`、`appName`、`appPublicKey` 字段）
- `LocalAppIdentityStore` interface（含 `loadCertificate`、`saveCertificate` 方法）
- `createLocalAppIdentityStore()` 函数
- `ensureLocalAppIdentity()` 函数（WebCrypto key generation）
- `LocalOfferSignature` interface
- `LocalOfferSigningInput` interface
- 签发 offer 的相关方法和类型

### 新增以下内容

```typescript
import type { RemoteRuntimeStorage } from './transport'

export interface MachineSessionStore {
  getSessionToken(machineId: string): string | null
  saveSessionToken(machineId: string, token: string, expiresAt: string): void
  clearSessionToken(machineId: string): void
}

export function createMachineSessionStore(
  storage: RemoteRuntimeStorage,
): MachineSessionStore {
  return {
    getSessionToken: (machineId) =>
      storage.getItem(`termx.session.${machineId}.token`),
    saveSessionToken: (machineId, token, expiresAt) => {
      storage.setItem(`termx.session.${machineId}.token`, token)
      storage.setItem(`termx.session.${machineId}.exp`, expiresAt)
    },
    clearSessionToken: (machineId) => {
      storage.removeItem(`termx.session.${machineId}.token`)
      storage.removeItem(`termx.session.${machineId}.exp`)
    },
  }
}
```

---

## 2. 修改 `remote-ui/src/managedHubApi.ts`

### 修改 `CreateSessionInput`

删除：`appCertificate: unknown`、`signature: { algorithm, nonce, timestamp, value }`  
新增：`sessionToken: string`

HTTP body 中：
- 删除 `app_certificate: ...` 和 `signature: ...` 行
- 新增 `session_token: input.sessionToken`

### 修改 `PairInput`

删除：`appPublicKey: string` 字段（不再需要 browser 公钥）

### 修改 `PairResult`（pairing 响应类型）

删除：`appCertificate: string`  
新增：`sessionToken: string`

响应解析处：
```typescript
// 旧
const appCertificate = record(response.app_certificate, 'app_certificate')
return { ..., appCertificate: JSON.stringify(appCertificate) }
// 新
const sessionToken = requiredString(response.session_token, 'session_token')
return { ..., sessionToken }
```

---

## 3. 修改 `remote-ui/src/managedHubRtcConnector.ts`

### 删除

- `ManagedHubRtcConnectorOptions.signOffer` 选项（函数类型字段）
- `ManagedHubRtcConnectInput.appCertificate` 字段
- `this.options.signOffer(...)` 调用代码块（通常在创建 offer 之后）

### 新增

- `ManagedHubRtcConnectInput.sessionToken: string`

修改 `connect()` 中 `api.createSession()` 调用：

```typescript
// 旧（含 signOffer 和 appCertificate）
const signed = await this.options.signOffer({ ... })
await this.options.api.createSession({
  ...,
  appCertificate: input.appCertificate,
  signature: signed,
})

// 新（直接传 sessionToken）
await this.options.api.createSession({
  ...,
  sessionToken: input.sessionToken,
})
```

---

## 4. 修改 `remote-ui/src/connectionOrchestrator.ts`

### 4a. 扩展 `ConnectionOrchestratorInput`

```typescript
export interface ConnectionOrchestratorInput {
  machineId: string
  terminalId?: string | undefined
  sessionToken?: string | undefined        // 新增
  hubUrls: string[]                        // 新增，替代 managed 中的单一 hub
  managed: {
    hubSessionId: string
    deviceId: string
  }
  onSnapshot?: ((snapshot: ConnectionAttemptSnapshot) => void) | undefined
}
```

### 4b. 修改 `connect()` 方法

将原来的顺序尝试（local → public_p2p → managed）改为：

```typescript
async connect(
  input: ConnectionOrchestratorInput,
  options: RtcConnectOptions = {},
): Promise<ConnectionOrchestratorResult> {
  const ac = new AbortController()
  const signal = combineSignals(options.signal, ac.signal)

  try {
    // 1. 优先尝试 local（2s 超时）
    const local = await this.tryLocalWithTimeout(input, { signal }, 2000)
    if (local) { ac.abort(); return local }

    // 2. 所有 hub_urls 并发竞速
    const hubUrls = input.hubUrls.length > 0 ? input.hubUrls : []
    if (hubUrls.length === 0) {
      throw new Error('no hub URLs configured')
    }
    input.onSnapshot?.({ stage: 'trying_managed', message: `Racing ${hubUrls.length} hub(s)` })

    const winner = await raceConnections(
      hubUrls.map((hubUrl) => () =>
        this.connectManagedHub(input, hubUrl, { signal }),
      ),
      signal,
    )
    if (winner) {
      ac.abort()
      input.onSnapshot?.({
        stage: 'connected', path: winner.path,
        relayInUse: winner.relayInUse, message: 'Connected',
      })
      return winner
    }
  } finally {
    ac.abort()
  }

  input.onSnapshot?.({ stage: 'failed', message: 'All connection paths failed' })
  throw new Error('all connection paths failed')
}
```

### 4c. 新增辅助函数（可放文件末尾）

```typescript
async function raceConnections(
  connectors: Array<() => Promise<ConnectionOrchestratorResult>>,
  signal?: AbortSignal,
): Promise<ConnectionOrchestratorResult | null> {
  if (connectors.length === 0) return null
  return new Promise((resolve) => {
    let settled = false
    let remaining = connectors.length
    for (const connector of connectors) {
      connector().then(
        (result) => { if (!settled) { settled = true; resolve(result) } },
        () => { remaining--; if (remaining === 0 && !settled) { settled = true; resolve(null) } },
      )
    }
    signal?.addEventListener('abort', () => {
      if (!settled) { settled = true; resolve(null) }
    })
  })
}

function combineSignals(...signals: Array<AbortSignal | undefined>): AbortSignal {
  const ac = new AbortController()
  for (const s of signals) {
    if (!s) continue
    if (s.aborted) { ac.abort(s.reason); return ac.signal }
    s.addEventListener('abort', () => ac.abort(s.reason))
  }
  return ac.signal
}
```

`connectManagedHub(input, hubUrl, options)` 方法：复用现有 managed 连接逻辑，但传入
`hubUrl` 而非全局配置的 hub URL。

`tryLocalWithTimeout(input, options, timeoutMs)` 方法：包裹现有 `connectLocal()`，
加 `Promise.race([connectLocal(...), sleep(timeoutMs).then(() => null)])`。
