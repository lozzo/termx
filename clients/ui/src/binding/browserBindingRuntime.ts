import { create } from '@bufbuild/protobuf'
import { openProtoEventSubscription } from '../core/protoEventSubscription'
import type { ExternalPairingAdapter, MachineRuntime, MachineRuntimeFactory } from '../app/RemoteControlApp'
import type { LocalStatus, RemoteNetworkRuntime, RemoteRuntimeStorage, RtcConnectOptions, TerminalInventoryEvents } from '../core/transport'
import type { Machine } from '../core/model'
import type { ProtoClientSession } from '../core/protoClientSession'
import { CommandEnvelopeSchema } from '../generated/apipb/application_pb'
import { ApplicationEventType, EventSubscribeCommandSchema } from '../generated/apipb/events_pb'
import { TerminalListCommandSchema, TerminalState } from '../generated/apipb/terminal_pb'
import { DeleteCredentialRequestSchema, ImportPairingRequestSchema } from '../generated/bindingpb/client_binding_pb'
import type { WebControlMachine } from '../api/webControlApi'
import { normalizeTerminalInventory } from '../terminal/terminalInventory'
import { createMachineStore } from '../state/machineStore'
import { BrowserCloudHttpPlatform } from './browserCloudHttpPlatform'
import { ProtoBindingClient, ProtoBindingConnector } from './protoBindingClient'
import { BrowserWasmLifecycle } from './browserWasmLifecycle'
import { BrowserWasmPlatform } from './browserWasmPlatform'
import { WasmBindingBackend } from './wasmBindingBackend'
import { TermxWasmRuntime, loadTermxWasmExports } from './wasmRuntime'

type BrowserBindingGeneration = {
  client: ProtoBindingClient
  close(): Promise<void>
}

/** BrowserBindingRuntime owns browser lifecycle generations while UI sees only ProtoClientSession. */
export class BrowserBindingRuntime {
  readonly machineRuntimeFactory: MachineRuntimeFactory
  readonly externalPairingAdapter: ExternalPairingAdapter

  private readonly cloud: BrowserCloudHttpPlatform
  private readonly lifecycle: BrowserWasmLifecycle<BrowserBindingGeneration>
  private generationCount = 0

  constructor(private readonly network: RemoteNetworkRuntime, private readonly storage: RemoteRuntimeStorage) {
    this.cloud = new BrowserCloudHttpPlatform(network.fetch, storage)
    this.lifecycle = new BrowserWasmLifecycle(async () => await this.createGeneration(), (generation) => {
      if (!generation) return
      this.generationCount += 1
      if (this.generationCount > 1) document.dispatchEvent(new Event('termx:resume'))
    })
    this.lifecycle.attach()
    this.machineRuntimeFactory = ({ machine }) => this.createMachineRuntime(machine)
    this.externalPairingAdapter = createBrowserPairingAdapter(this, storage)
  }

  async client(): Promise<ProtoBindingClient> {
    if (document.visibilityState === 'hidden') throw new Error('browser client is suspended while the page is hidden')
    return (this.lifecycle.current ?? await this.lifecycle.start()).client
  }

  registerMachine(machine: WebControlMachine): void {
    const stored = createMachineStore({ storage: this.storage as Storage }).getMachine(machine.id)
    const hubUrl = machine.currentHubUrl || machine.hubUrls[0] || stored?.endpoints.hub
    if (!hubUrl) throw new Error(`managed endpoint ${machine.id} has no Hub URL`)
    this.cloud.registerEndpoint({ endpointId: machine.id, hubUrl })
  }

  async dispose(): Promise<void> { await this.lifecycle.dispose() }

  private async createGeneration(): Promise<BrowserBindingGeneration> {
    const exports = await loadTermxWasmExports({ wasmUrl: '/termx-wasm/termx-client.wasm', wasmExecUrl: '/termx-wasm/wasm_exec.js' })
    let runtime: TermxWasmRuntime | null = null
    const platform = new BrowserWasmPlatform(this.cloud, async (payload) => {
      if (!runtime) throw new Error('browser WASM generation is unavailable')
      await runtime.platformEvent(payload)
    })
    runtime = await TermxWasmRuntime.create(exports, platform)
    const client = new ProtoBindingClient(new WasmBindingBackend(runtime))
    return { client, close: () => client.close() }
  }

  private createMachineRuntime(machine: WebControlMachine): MachineRuntime {
    this.registerMachine(machine)
    const connector = this.createConnector(machine)
    return {
      api: {
        getStatus: async () => machineStatus(machine, this.storage),
        listTerminals: async (options) => await listTerminals(machine.id, connector, options),
      },
      connector: { connect: (target, options) => connector.connect(target, options) },
      inventoryEvents: createInventoryEvents(machine.id, connector),
    }
  }

  private createConnector(machine: WebControlMachine): { connect(input: { machineId: string }, options?: RtcConnectOptions): Promise<ProtoClientSession> } {
    const key = (field: string) => this.storage.getItem(`termx.endpoint.${machine.id}.${field}`)?.trim() ?? ''
    const targetDeviceId = key('targetDeviceId') || machine.id
    const deviceFingerprint = key('deviceFingerprint')
    const credentialRef = key('grantRef')
    const relayMode = key('relayMode')
    if (!deviceFingerprint || !credentialRef) return { connect: async () => { throw new Error('Managed endpoint requires a current pairing grant') } }
    if (relayMode && relayMode !== 'auto' && relayMode !== 'direct' && relayMode !== 'relay_only' && relayMode !== 'smart_route') {
      return { connect: async () => { throw new Error('Managed endpoint has an invalid relay mode') } }
    }
    return {
      connect: async (input, options) => {
        const binding = new ProtoBindingConnector(() => {
          const active = this.lifecycle.current?.client
          if (!active) throw new Error('browser WASM generation is unavailable')
          return active
        }, {
          endpointId: machine.id,
          targetDeviceId,
          deviceFingerprint,
          credentialRef,
          relayMode: relayMode === 'relay_only' || relayMode === 'smart_route' || relayMode === 'auto' ? relayMode : 'direct',
        })
        await this.client()
        return await binding.connect(input, bindingConnectOptions(options))
      },
    }
  }
}

function bindingConnectOptions(options: RtcConnectOptions | undefined) {
  if (!options) return undefined
  return {
    ...(options.signal ? { signal: options.signal } : {}),
    ...(options.forceRelay !== undefined ? { forceRelay: options.forceRelay } : {}),
    ...(options.onStatus ? { onStatus: options.onStatus } : {}),
    ...(options.onConnectionState ? { onConnectionState: options.onConnectionState } : {}),
  }
}

function createBrowserPairingAdapter(runtime: BrowserBindingRuntime, storage: RemoteRuntimeStorage): ExternalPairingAdapter {
  const key = (machineId: string, field: string) => `termx.endpoint.${machineId}.${field}`
  return {
    async import(rawValue, expectedMachineId) {
      const imported = await (await runtime.client()).importPairing(create(ImportPairingRequestSchema, {
        requestId: crypto.randomUUID(), portablePayload: rawValue, expectedEndpointId: expectedMachineId ?? '',
      }))
      if (!imported.endpointId || !imported.credentialRef) return null
      const expiresAt = new Date(Number(imported.expiresAtUnixNano / 1_000_000n)).toISOString()
      storage.setItem(key(imported.endpointId, 'targetDeviceId'), imported.targetDeviceId)
      storage.setItem(key(imported.endpointId, 'deviceFingerprint'), imported.deviceFingerprint)
      storage.setItem(key(imported.endpointId, 'grantRef'), imported.credentialRef)
      storage.setItem(key(imported.endpointId, 'grantExpiresAt'), expiresAt)
      storage.setItem(key(imported.endpointId, 'relayMode'), 'direct')
      return { machine: { id: imported.endpointId, name: imported.label || imported.endpointId, accessClass: 'cloud' }, expiresAt }
    },
    isAuthorized(machineId) {
      return Boolean(storage.getItem(key(machineId, 'targetDeviceId'))?.trim() && storage.getItem(key(machineId, 'deviceFingerprint'))?.trim() && storage.getItem(key(machineId, 'grantRef'))?.trim())
    },
    authorizationExpiresAt(machineId) { return storage.getItem(key(machineId, 'grantExpiresAt'))?.trim() || undefined },
    async forget(machineId) {
      const credentialRef = storage.getItem(key(machineId, 'grantRef'))?.trim()
      for (const field of ['targetDeviceId', 'deviceFingerprint', 'grantRef', 'grantExpiresAt', 'relayMode']) storage.removeItem(key(machineId, field))
      if (credentialRef) await (await runtime.client()).deleteCredential(create(DeleteCredentialRequestSchema, { requestId: crypto.randomUUID(), credentialRef }))
    },
  }
}

async function listTerminals(machineId: string, connector: { connect(input: { machineId: string }, options?: RtcConnectOptions): Promise<ProtoClientSession> }, options?: RtcConnectOptions) {
  const session = await connector.connect({ machineId }, options)
  try {
    const result = await session.execute(create(CommandEnvelopeSchema, { command: { case: 'terminalList', value: create(TerminalListCommandSchema) } }))
    if (result.result.case !== 'terminalList') throw new Error('terminal list returned no result')
    return normalizeTerminalInventory({
      machine_id: machineId,
      terminals: result.result.value.terminals.map((terminal) => ({
        terminal_id: terminal.ref?.terminalId ?? '', name: terminal.name,
        state: terminal.state === TerminalState.RUNNING ? 'running' : terminal.state === TerminalState.EXITED ? 'exited' : 'unknown',
        command: terminal.command, cwd: terminal.cwd, live_cwd: terminal.liveCwd, cols: terminal.size?.cols ?? 0, rows: terminal.size?.rows ?? 0,
      })),
    }).terminals
  } finally {
    await session.close()
  }
}

function createInventoryEvents(machineId: string, connector: { connect(input: { machineId: string }): Promise<ProtoClientSession> }): TerminalInventoryEvents {
  return {
    subscribe(targetMachineId, handler) {
      if (targetMachineId !== machineId) return { close() {} }
      let closed = false
      let session: ProtoClientSession | null = null
      let subscription: { close(): void } | null = null
      void connector.connect({ machineId }).then(async (connected) => {
        if (closed) { void connected.close(); return }
        session = connected
        subscription = await openProtoEventSubscription(connected, create(EventSubscribeCommandSchema, {
          types: [ApplicationEventType.TERMINAL_LIFECYCLE],
        }), (event) => {
          if (event.event.case === 'terminalLifecycle') handler({ type: 'inventory_changed', payload: event.event.value })
        })
      }).catch(() => undefined)
      return { close() { closed = true; subscription?.close(); subscription = null; void session?.close(); session = null } }
    },
  }
}

function machineStatus(machine: WebControlMachine, storage: RemoteRuntimeStorage): LocalStatus {
  const stored = createMachineStore({ storage: storage as Storage }).getMachine(machine.id)
  const statusMachine: Machine = {
    machineId: machine.id, name: machine.name, state: machine.online ? 'online' : 'offline', terminalCount: stored?.terminalCount,
    ...(machine.lastSeen || stored?.lastSeenAt ? { lastSeenAt: machine.lastSeen ?? stored?.lastSeenAt } : {}),
  }
  return { machine: statusMachine, localWeb: { httpUrl: machine.controlUrl ?? stored?.endpoints.webControl ?? '', rtcOfferUrl: machine.currentHubUrl ?? machine.hubUrls[0] ?? stored?.endpoints.hub ?? '' } }
}
