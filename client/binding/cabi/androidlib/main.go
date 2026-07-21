// Package main 生成 Android 使用的 termx Client Engine c-shared library。
// 稳定 C ABI 只传递 serialized Proto 与 opaque handle；Android spike 的 in-process daemon 仅用于证明真实 Pion/auth/Hello/API 链路。
package main

/*
#include <stdlib.h>
#include <stddef.h>
#include <stdint.h>

typedef uint64_t termx_handle_t;
typedef enum termx_status_v1 {
  TERMX_STATUS_OK = 0,
  TERMX_STATUS_INVALID_ARGUMENT = 1,
  TERMX_STATUS_INVALID_HANDLE = 2,
  TERMX_STATUS_CLOSED = 3,
  TERMX_STATUS_CAPACITY = 4,
  TERMX_STATUS_INTERNAL = 5
} termx_status_v1;
typedef struct termx_buffer_v1 {
  termx_handle_t buffer_handle;
  const uint8_t *data;
  size_t length;
} termx_buffer_v1;
*/
import "C"

import (
	"context"
	"errors"
	"sync"
	"time"
	"unsafe"

	"github.com/muxvia/muxvia/client/binding"
)

var androidLibrary = struct {
	sync.Mutex
	registry   *binding.Registry
	runtimeDir string
	hosts      map[uint64]androidHost
	platforms  map[uint64]*binding.PlatformBroker
	buffers    map[uint64]unsafe.Pointer
	nextBuffer uint64
}{
	registry: binding.NewRegistry(), hosts: make(map[uint64]androidHost),
	platforms: make(map[uint64]*binding.PlatformBroker), buffers: make(map[uint64]unsafe.Pointer),
}

type androidHost interface {
	binding.Host
	close() error
}

//export termx_android_spike_set_runtime_dir
func termx_android_spike_set_runtime_dir(value *C.char) C.termx_status_v1 {
	if value == nil {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	androidLibrary.Lock()
	androidLibrary.runtimeDir = C.GoString(value)
	androidLibrary.Unlock()
	return C.TERMX_STATUS_OK
}

//export termx_client_abi_version
func termx_client_abi_version() C.uint32_t { return C.uint32_t(binding.ABIVersion) }

//export termx_engine_create
func termx_engine_create(out *C.termx_handle_t) C.termx_status_v1 {
	if out == nil {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	host := newAndroidProductionHost()
	return createAndroidEngine(host, host.broker, out)
}

//export termx_android_spike_engine_create
func termx_android_spike_engine_create(out *C.termx_handle_t) C.termx_status_v1 {
	if out == nil {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	androidLibrary.Lock()
	runtimeDir := androidLibrary.runtimeDir
	androidLibrary.Unlock()
	host, err := newAndroidSpikeHost(runtimeDir)
	if err != nil {
		return status(err)
	}
	return createAndroidEngine(host, nil, out)
}

func createAndroidEngine(host androidHost, broker *binding.PlatformBroker, out *C.termx_handle_t) C.termx_status_v1 {
	handle, err := androidLibrary.registry.CreateEngine(host)
	if err != nil {
		_ = host.close()
		return status(err)
	}
	androidLibrary.Lock()
	androidLibrary.hosts[handle] = host
	if broker != nil {
		androidLibrary.platforms[handle] = broker
	}
	androidLibrary.Unlock()
	*out = C.termx_handle_t(handle)
	return C.TERMX_STATUS_OK
}

//export termx_engine_open_session
func termx_engine_open_session(engine C.termx_handle_t, data *C.uint8_t, length C.size_t, out *C.termx_handle_t) C.termx_status_v1 {
	return operation(data, length, out, func(payload []byte) (uint64, error) {
		return androidLibrary.registry.OpenSession(uint64(engine), payload)
	})
}

//export termx_engine_execute
func termx_engine_execute(engine, session C.termx_handle_t, data *C.uint8_t, length C.size_t, out *C.termx_handle_t) C.termx_status_v1 {
	return operation(data, length, out, func(payload []byte) (uint64, error) {
		return androidLibrary.registry.Execute(uint64(engine), uint64(session), payload)
	})
}

//export termx_engine_open_resource_stream
func termx_engine_open_resource_stream(engine, session C.termx_handle_t, data *C.uint8_t, length C.size_t, out *C.termx_handle_t) C.termx_status_v1 {
	return operation(data, length, out, func(payload []byte) (uint64, error) {
		return androidLibrary.registry.OpenResourceStream(uint64(engine), uint64(session), payload)
	})
}

//export termx_engine_send_resource_stream_frame
func termx_engine_send_resource_stream_frame(engine, stream C.termx_handle_t, data *C.uint8_t, length C.size_t) C.termx_status_v1 {
	if data == nil || length == 0 || uint64(length) > uint64(binding.MaxPayloadBytes) {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	payload := C.GoBytes(unsafe.Pointer(data), C.int(length))
	return status(androidLibrary.registry.SendResourceStreamFrame(uint64(engine), uint64(stream), payload))
}

//export termx_engine_close_resource_stream
func termx_engine_close_resource_stream(engine, stream C.termx_handle_t) C.termx_status_v1 {
	return status(androidLibrary.registry.CloseResourceStream(uint64(engine), uint64(stream)))
}

//export termx_engine_command
func termx_engine_command(engine C.termx_handle_t, data *C.uint8_t, length C.size_t, out *C.termx_handle_t) C.termx_status_v1 {
	return operation(data, length, out, func(payload []byte) (uint64, error) {
		return androidLibrary.registry.EngineCommand(uint64(engine), payload)
	})
}

//export termx_engine_next_event
func termx_engine_next_event(engine C.termx_handle_t, timeoutMillis C.uint32_t, out *C.termx_buffer_v1) C.termx_status_v1 {
	if out == nil {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	ctx := context.Background()
	cancel := func() {}
	if timeoutMillis > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMillis)*time.Millisecond)
	}
	defer cancel()
	payload, err := androidLibrary.registry.NextEvent(ctx, uint64(engine))
	if err != nil {
		return status(err)
	}
	return allocateBuffer(payload, out)
}

//export termx_platform_next_request
func termx_platform_next_request(engine C.termx_handle_t, timeoutMillis C.uint32_t, out *C.termx_buffer_v1) C.termx_status_v1 {
	if out == nil {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	androidLibrary.Lock()
	broker := androidLibrary.platforms[uint64(engine)]
	androidLibrary.Unlock()
	if broker == nil {
		return C.TERMX_STATUS_INVALID_HANDLE
	}
	ctx := context.Background()
	cancel := func() {}
	if timeoutMillis > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMillis)*time.Millisecond)
	}
	defer cancel()
	payload, err := broker.NextRequest(ctx)
	if err != nil {
		return status(err)
	}
	return allocateBuffer(payload, out)
}

//export termx_platform_complete
func termx_platform_complete(engine C.termx_handle_t, data *C.uint8_t, length C.size_t) C.termx_status_v1 {
	if data == nil || length == 0 || uint64(length) > uint64(binding.MaxPayloadBytes) {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	androidLibrary.Lock()
	broker := androidLibrary.platforms[uint64(engine)]
	androidLibrary.Unlock()
	if broker == nil {
		return C.TERMX_STATUS_INVALID_HANDLE
	}
	return status(broker.Complete(C.GoBytes(unsafe.Pointer(data), C.int(length))))
}

func allocateBuffer(payload []byte, out *C.termx_buffer_v1) C.termx_status_v1 {
	pointer := C.malloc(C.size_t(len(payload)))
	if pointer == nil {
		return C.TERMX_STATUS_CAPACITY
	}
	copy(unsafe.Slice((*byte)(pointer), len(payload)), payload)
	androidLibrary.Lock()
	androidLibrary.nextBuffer++
	handle := androidLibrary.nextBuffer
	if handle == 0 {
		androidLibrary.Unlock()
		C.free(pointer)
		return C.TERMX_STATUS_CAPACITY
	}
	androidLibrary.buffers[handle] = pointer
	androidLibrary.Unlock()
	out.buffer_handle = C.termx_handle_t(handle)
	out.data = (*C.uint8_t)(pointer)
	out.length = C.size_t(len(payload))
	return C.TERMX_STATUS_OK
}

//export termx_engine_cancel
func termx_engine_cancel(engine, operationHandle C.termx_handle_t) C.termx_status_v1 {
	return status(androidLibrary.registry.Cancel(uint64(engine), uint64(operationHandle)))
}

//export termx_engine_close_session
func termx_engine_close_session(engine, session C.termx_handle_t) C.termx_status_v1 {
	return status(androidLibrary.registry.CloseSession(uint64(engine), uint64(session)))
}

//export termx_engine_release
func termx_engine_release(engine, handle C.termx_handle_t) C.termx_status_v1 {
	return status(androidLibrary.registry.Release(uint64(engine), uint64(handle)))
}

//export termx_engine_close
func termx_engine_close(engine C.termx_handle_t) C.termx_status_v1 {
	handle := uint64(engine)
	err := androidLibrary.registry.CloseEngine(handle)
	androidLibrary.Lock()
	host := androidLibrary.hosts[handle]
	delete(androidLibrary.hosts, handle)
	broker := androidLibrary.platforms[handle]
	delete(androidLibrary.platforms, handle)
	androidLibrary.Unlock()
	if broker != nil {
		_ = broker.Close()
	}
	if host != nil {
		if closeErr := host.close(); err == nil {
			err = closeErr
		}
	}
	return status(err)
}

//export termx_buffer_free
func termx_buffer_free(buffer C.termx_handle_t) C.termx_status_v1 {
	handle := uint64(buffer)
	androidLibrary.Lock()
	pointer := androidLibrary.buffers[handle]
	if pointer != nil {
		delete(androidLibrary.buffers, handle)
	}
	androidLibrary.Unlock()
	if pointer == nil {
		return C.TERMX_STATUS_INVALID_HANDLE
	}
	C.free(pointer)
	return C.TERMX_STATUS_OK
}

func operation(data *C.uint8_t, length C.size_t, out *C.termx_handle_t, invoke func([]byte) (uint64, error)) C.termx_status_v1 {
	if data == nil || length == 0 || uint64(length) > uint64(binding.MaxPayloadBytes) || out == nil {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	payload := C.GoBytes(unsafe.Pointer(data), C.int(length))
	handle, err := invoke(payload)
	if err != nil {
		return status(err)
	}
	*out = C.termx_handle_t(handle)
	return C.TERMX_STATUS_OK
}

func status(err error) C.termx_status_v1 {
	if err == nil {
		return C.TERMX_STATUS_OK
	}
	switch {
	case errors.Is(err, binding.ErrInvalidHandle):
		return C.TERMX_STATUS_INVALID_HANDLE
	case errors.Is(err, binding.ErrClosed):
		return C.TERMX_STATUS_CLOSED
	case errors.Is(err, binding.ErrHandleActive):
		return C.TERMX_STATUS_INVALID_ARGUMENT
	default:
		return C.TERMX_STATUS_INTERNAL
	}
}

func main() {}
