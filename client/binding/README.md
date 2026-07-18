# Client Binding ABI

`client/binding` is the stable outer boundary for Android JNI, WebAssembly, and future Swift/Desktop wrappers.

## Payloads

- `OpenSession` input is serialized `bindingpb.OpenSessionRequest`.
- `Execute` input is serialized `apipb.CommandEnvelope`.
- `NextEvent` output is serialized `bindingpb.EventEnvelope`.
- Business commands, results, events, errors, cancellation, and daemon resources remain generated Proto contracts.
- Opaque `uint64` handles identify only binding-local engine, operation, session, and output-buffer ownership.

The ABI must not add command-specific entry points such as terminal list, history copy, or file upload. New business behavior changes Proto, not C/JNI/WASM symbols.

## Ownership

- Input bytes are borrowed for the synchronous call and copied before asynchronous work starts.
- C output buffers must be allocated outside Go-managed memory and released exactly once with `termx_buffer_free`.
- WASM wrappers return copied `Uint8Array` content in `{status, handle?, payload?, error?}` results; operations that can wait on platform or transport work return `Promise`.
- Completed operation handles and closed session handles remain registered until explicit `Release`.
- Daemon resources use `apipb.ReleaseResourceCommand`; binding `Release` never releases daemon-owned resources.

## Concurrency

- Open and execute return immediately with an operation handle.
- Results and application events are drained through one ordered, bounded event queue.
- Queue saturation applies backpressure and never drops results or events.
- Cancel is operation-scoped. Closing a session does not affect another session or select a fallback route.
- Closing an engine cancels all operations, closes all sessions, and unblocks event waiters.

## Platform Primitives

- Android and Web both use `managedhost.Host` for endpoint generation, Cloud signaling, credential resolution, remote auth, Hello, Proto API, session, and resource ownership.
- Android injects the native Pion peer factory. Web injects `adapter/managed/platform.Factory`, which exchanges serialized `bindingpb.PlatformRequest`, `PlatformResponse`, and `PlatformEvent` with the browser adapter.
- Browser JavaScript owns only `RTCPeerConnection`, `RTCDataChannel`, WebCrypto/IndexedDB, and page lifecycle primitives. It cannot interpret remote-auth or application payloads.
- Browser channel binding is accepted only after the actual certificate from `RTCDtlsTransport.getRemoteCertificates()` hashes to the SHA-256 fingerprint declared by the applied remote SDP.
- Android background/screen-off and browser hidden/pagehide/freeze destroy the current engine generation. Resume creates a new engine before publishing a usable binding to UI code.

The C declarations in `cabi/termx_client.h` and names in `wasm_exports.txt` are stable ABI baselines. `cabi/androidlib` and `wasmlib` are the real platform wrappers; neither may add business-specific exports.
