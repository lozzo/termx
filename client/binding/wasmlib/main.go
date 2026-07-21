//go:build js && wasm

// Package main 生成浏览器使用的 Go Client Engine WebAssembly module。
// 导出层只接收 Uint8Array、number opaque handle 和异步 status result，不解释 application Proto。
package main

import (
	"context"
	"errors"
	"sync"
	"syscall/js"

	platformpeer "github.com/muxvia/muxvia/client/adapter/managed/platform"
	"github.com/muxvia/muxvia/client/binding"
	"github.com/muxvia/muxvia/client/binding/enginehost"
	clientruntime "github.com/muxvia/muxvia/client/runtime"
)

const (
	statusOK              = 0
	statusInvalidArgument = 1
	statusInvalidHandle   = 2
	statusClosed          = 3
	statusCapacity        = 4
	statusInternal        = 5
)

var wasmLibrary = struct {
	sync.Mutex
	registry *binding.Registry
	hosts    map[uint64]*wasmHost
}{registry: binding.NewRegistry(), hosts: make(map[uint64]*wasmHost)}

var wasmSessionAuthority = clientruntime.NewSessionGenerationAuthority()

type wasmHost struct {
	*enginehost.Host
	peers *platformpeer.Factory
}

func main() {
	exports := map[string]any{
		"muxviaClientAbiVersion":              syncExport(func([]js.Value) (js.Value, error) { return js.ValueOf(binding.ABIVersion), nil }),
		"muxviaEngineCreate":                  syncExport(engineCreate),
		"muxviaEngineOpenSession":             syncExport(payloadOperation(wasmLibrary.registry.OpenSession)),
		"muxviaEngineExecute":                 syncExport(sessionPayloadOperation(wasmLibrary.registry.Execute)),
		"muxviaEngineOpenResourceStream":      syncExport(sessionPayloadOperation(wasmLibrary.registry.OpenResourceStream)),
		"muxviaEngineSendResourceStreamFrame": asyncExport(sendResourceStreamFrame),
		"muxviaEngineCloseResourceStream":     asyncExport(handleOperation(wasmLibrary.registry.CloseResourceStream)),
		"muxviaEngineCommand":                 syncExport(payloadOperation(wasmLibrary.registry.EngineCommand)),
		"muxviaEngineNextEvent":               asyncExport(nextEvent),
		"muxviaPlatformNextRequest":           asyncExport(nextPlatformRequest),
		"muxviaPlatformComplete":              syncExport(completePlatformRequest),
		"muxviaPlatformEvent":                 asyncExport(handlePlatformEvent),
		"muxviaEngineCancel":                  syncExport(handleOperation(wasmLibrary.registry.Cancel)),
		"muxviaEngineCloseSession":            asyncExport(handleOperation(wasmLibrary.registry.CloseSession)),
		"muxviaEngineRelease":                 syncExport(handleOperation(wasmLibrary.registry.Release)),
		"muxviaEngineClose":                   asyncExport(engineClose),
		"muxviaBufferFree":                    syncExport(func([]js.Value) (js.Value, error) { return resultObject(statusOK, 0, nil, ""), nil }),
	}
	for name, value := range exports {
		js.Global().Set(name, value)
	}
	select {}
}

func engineCreate(_ []js.Value) (js.Value, error) {
	broker := binding.NewPlatformBroker()
	peers, err := platformpeer.NewFactory(broker)
	if err != nil {
		return js.Undefined(), err
	}
	host, err := enginehost.New(enginehost.Options{
		Broker: broker, ManagedPeers: peers, ClientName: "muxvia-web", CredentialPrefix: "web-access-",
		SessionAuthority: wasmSessionAuthority,
	})
	if err != nil {
		_ = peers.Close()
		return js.Undefined(), err
	}
	value := &wasmHost{Host: host, peers: peers}
	handle, err := wasmLibrary.registry.CreateEngine(value)
	if err != nil {
		_ = value.Close()
		return js.Undefined(), err
	}
	wasmLibrary.Lock()
	wasmLibrary.hosts[handle] = value
	wasmLibrary.Unlock()
	return resultObject(statusOK, handle, nil, ""), nil
}

func payloadOperation(operation func(uint64, []byte) (uint64, error)) func([]js.Value) (js.Value, error) {
	return func(args []js.Value) (js.Value, error) {
		if len(args) != 2 {
			return js.Undefined(), binding.ErrInvalidHandle
		}
		handle, err := jsHandle(args[0])
		if err != nil {
			return js.Undefined(), err
		}
		payload, err := jsBytes(args[1])
		if err != nil {
			return js.Undefined(), err
		}
		result, err := operation(handle, payload)
		if err != nil {
			return js.Undefined(), err
		}
		return resultObject(statusOK, result, nil, ""), nil
	}
}

func sessionPayloadOperation(operation func(uint64, uint64, []byte) (uint64, error)) func([]js.Value) (js.Value, error) {
	return func(args []js.Value) (js.Value, error) {
		if len(args) != 3 {
			return js.Undefined(), binding.ErrInvalidHandle
		}
		engine, err := jsHandle(args[0])
		if err != nil {
			return js.Undefined(), err
		}
		session, err := jsHandle(args[1])
		if err != nil {
			return js.Undefined(), err
		}
		payload, err := jsBytes(args[2])
		if err != nil {
			return js.Undefined(), err
		}
		result, err := operation(engine, session, payload)
		if err != nil {
			return js.Undefined(), err
		}
		return resultObject(statusOK, result, nil, ""), nil
	}
}

func handleOperation(operation func(uint64, uint64) error) func([]js.Value) (js.Value, error) {
	return func(args []js.Value) (js.Value, error) {
		if len(args) != 2 {
			return js.Undefined(), binding.ErrInvalidHandle
		}
		engine, err := jsHandle(args[0])
		if err != nil {
			return js.Undefined(), err
		}
		handle, err := jsHandle(args[1])
		if err != nil {
			return js.Undefined(), err
		}
		if err := operation(engine, handle); err != nil {
			return js.Undefined(), err
		}
		return resultObject(statusOK, 0, nil, ""), nil
	}
}

func sendResourceStreamFrame(args []js.Value) (js.Value, error) {
	if len(args) != 3 {
		return js.Undefined(), binding.ErrInvalidHandle
	}
	engine, err := jsHandle(args[0])
	if err != nil {
		return js.Undefined(), err
	}
	stream, err := jsHandle(args[1])
	if err != nil {
		return js.Undefined(), err
	}
	payload, err := jsBytes(args[2])
	if err != nil {
		return js.Undefined(), err
	}
	if err := wasmLibrary.registry.SendResourceStreamFrame(engine, stream, payload); err != nil {
		return js.Undefined(), err
	}
	return resultObject(statusOK, 0, nil, ""), nil
}

func nextEvent(args []js.Value) (js.Value, error) {
	if len(args) != 1 {
		return js.Undefined(), binding.ErrInvalidHandle
	}
	engine, err := jsHandle(args[0])
	if err != nil {
		return js.Undefined(), err
	}
	payload, err := wasmLibrary.registry.NextEvent(context.Background(), engine)
	if err != nil {
		return js.Undefined(), err
	}
	return resultObject(statusOK, 0, payload, ""), nil
}

func nextPlatformRequest(args []js.Value) (js.Value, error) {
	if len(args) != 1 {
		return js.Undefined(), binding.ErrInvalidHandle
	}
	engine, host, err := activeHost(args[0])
	if err != nil {
		return js.Undefined(), err
	}
	payload, err := host.Broker().NextRequest(context.Background())
	if err != nil {
		return js.Undefined(), err
	}
	_ = engine
	return resultObject(statusOK, 0, payload, ""), nil
}

func completePlatformRequest(args []js.Value) (js.Value, error) {
	if len(args) != 2 {
		return js.Undefined(), binding.ErrInvalidHandle
	}
	_, host, err := activeHost(args[0])
	if err != nil {
		return js.Undefined(), err
	}
	payload, err := jsBytes(args[1])
	if err != nil {
		return js.Undefined(), err
	}
	if err := host.Broker().Complete(payload); err != nil {
		return js.Undefined(), err
	}
	return resultObject(statusOK, 0, nil, ""), nil
}

func handlePlatformEvent(args []js.Value) (js.Value, error) {
	if len(args) != 2 {
		return js.Undefined(), binding.ErrInvalidHandle
	}
	_, host, err := activeHost(args[0])
	if err != nil {
		return js.Undefined(), err
	}
	payload, err := jsBytes(args[1])
	if err != nil {
		return js.Undefined(), err
	}
	if err := host.peers.HandleEvent(payload); err != nil {
		return js.Undefined(), err
	}
	return resultObject(statusOK, 0, nil, ""), nil
}

func engineClose(args []js.Value) (js.Value, error) {
	if len(args) != 1 {
		return js.Undefined(), binding.ErrInvalidHandle
	}
	engine, err := jsHandle(args[0])
	if err != nil {
		return js.Undefined(), err
	}
	if err := wasmLibrary.registry.CloseEngine(engine); err != nil {
		return js.Undefined(), err
	}
	wasmLibrary.Lock()
	host := wasmLibrary.hosts[engine]
	delete(wasmLibrary.hosts, engine)
	wasmLibrary.Unlock()
	if host != nil {
		_ = host.Close()
	}
	return resultObject(statusOK, 0, nil, ""), nil
}

func activeHost(value js.Value) (uint64, *wasmHost, error) {
	engine, err := jsHandle(value)
	if err != nil {
		return 0, nil, err
	}
	wasmLibrary.Lock()
	host := wasmLibrary.hosts[engine]
	wasmLibrary.Unlock()
	if host == nil {
		return 0, nil, binding.ErrInvalidHandle
	}
	return engine, host, nil
}

func syncExport(operation func([]js.Value) (js.Value, error)) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any {
		value, err := operation(args)
		if err != nil {
			return resultObject(statusOf(err), 0, nil, err.Error())
		}
		return value
	})
}

func asyncExport(operation func([]js.Value) (js.Value, error)) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any {
		constructor := js.Global().Get("Promise")
		executor := js.FuncOf(func(_ js.Value, promiseArgs []js.Value) any {
			resolve := promiseArgs[0]
			copied := append([]js.Value(nil), args...)
			go func() {
				value, err := operation(copied)
				if err != nil {
					resolve.Invoke(resultObject(statusOf(err), 0, nil, err.Error()))
					return
				}
				resolve.Invoke(value)
			}()
			return nil
		})
		promise := constructor.New(executor)
		executor.Release()
		return promise
	})
}

func resultObject(status int, handle uint64, payload []byte, message string) js.Value {
	result := js.Global().Get("Object").New()
	result.Set("status", status)
	if handle != 0 {
		result.Set("handle", float64(handle))
	}
	if payload != nil {
		array := js.Global().Get("Uint8Array").New(len(payload))
		js.CopyBytesToJS(array, payload)
		result.Set("payload", array)
	}
	if message != "" {
		result.Set("error", message)
	}
	return result
}

func jsHandle(value js.Value) (uint64, error) {
	if value.Type() != js.TypeNumber {
		return 0, binding.ErrInvalidHandle
	}
	number := value.Float()
	if number <= 0 || number > 9_007_199_254_740_991 || number != float64(uint64(number)) {
		return 0, binding.ErrInvalidHandle
	}
	return uint64(number), nil
}

func jsBytes(value js.Value) ([]byte, error) {
	if value.Type() != js.TypeObject || !value.InstanceOf(js.Global().Get("Uint8Array")) {
		return nil, errors.New("binding payload must be Uint8Array")
	}
	if value.Get("byteLength").Int() <= 0 || value.Get("byteLength").Int() > binding.MaxPayloadBytes {
		return nil, errors.New("binding payload length is invalid")
	}
	payload := make([]byte, value.Get("byteLength").Int())
	if copied := js.CopyBytesToGo(payload, value); copied != len(payload) {
		return nil, errors.New("binding payload copy was incomplete")
	}
	return payload, nil
}

func statusOf(err error) int {
	switch {
	case err == nil:
		return statusOK
	case errors.Is(err, binding.ErrInvalidHandle):
		return statusInvalidHandle
	case errors.Is(err, binding.ErrClosed), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return statusClosed
	case errors.Is(err, binding.ErrHandleActive):
		return statusCapacity
	default:
		return statusInternal
	}
}
