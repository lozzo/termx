//go:build cgo

// Package main 生成 Android 使用的 muxvia Client Engine c-shared library。
// 稳定 C ABI 只传递 serialized Proto 与 opaque handle；网络与授权状态机由 Go Client Engine 持有。
package main

/*
#include <stdlib.h>
#include <stddef.h>
#include <stdint.h>

typedef uint64_t muxvia_handle_t;
typedef enum muxvia_status_v1 {
  MUXVIA_STATUS_OK = 0,
  MUXVIA_STATUS_INVALID_ARGUMENT = 1,
  MUXVIA_STATUS_INVALID_HANDLE = 2,
  MUXVIA_STATUS_CLOSED = 3,
  MUXVIA_STATUS_CAPACITY = 4,
  MUXVIA_STATUS_INTERNAL = 5
} muxvia_status_v1;
typedef struct muxvia_buffer_v1 {
  muxvia_handle_t buffer_handle;
  const uint8_t *data;
  size_t length;
} muxvia_buffer_v1;
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

//export muxvia_android_spike_set_runtime_dir
func muxvia_android_spike_set_runtime_dir(value *C.char) C.muxvia_status_v1 {
	if value == nil {
		return C.MUXVIA_STATUS_INVALID_ARGUMENT
	}
	androidLibrary.Lock()
	androidLibrary.runtimeDir = C.GoString(value)
	androidLibrary.Unlock()
	return C.MUXVIA_STATUS_OK
}

//export muxvia_client_abi_version
func muxvia_client_abi_version() C.uint32_t { return C.uint32_t(binding.ABIVersion) }

//export muxvia_engine_create
func muxvia_engine_create(out *C.muxvia_handle_t) C.muxvia_status_v1 {
	if out == nil {
		return C.MUXVIA_STATUS_INVALID_ARGUMENT
	}
	host := newAndroidProductionHost()
	return createAndroidEngine(host, host.broker, out)
}

//export muxvia_android_spike_engine_create
func muxvia_android_spike_engine_create(out *C.muxvia_handle_t) C.muxvia_status_v1 {
	if out == nil {
		return C.MUXVIA_STATUS_INVALID_ARGUMENT
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

func createAndroidEngine(host androidHost, broker *binding.PlatformBroker, out *C.muxvia_handle_t) C.muxvia_status_v1 {
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
	*out = C.muxvia_handle_t(handle)
	return C.MUXVIA_STATUS_OK
}

//export muxvia_engine_open_session
func muxvia_engine_open_session(engine C.muxvia_handle_t, data *C.uint8_t, length C.size_t, out *C.muxvia_handle_t) C.muxvia_status_v1 {
	return operation(data, length, out, func(payload []byte) (uint64, error) {
		return androidLibrary.registry.OpenSession(uint64(engine), payload)
	})
}

//export muxvia_engine_execute
func muxvia_engine_execute(engine, session C.muxvia_handle_t, data *C.uint8_t, length C.size_t, out *C.muxvia_handle_t) C.muxvia_status_v1 {
	return operation(data, length, out, func(payload []byte) (uint64, error) {
		return androidLibrary.registry.Execute(uint64(engine), uint64(session), payload)
	})
}

//export muxvia_engine_open_resource_stream
func muxvia_engine_open_resource_stream(engine, session C.muxvia_handle_t, data *C.uint8_t, length C.size_t, out *C.muxvia_handle_t) C.muxvia_status_v1 {
	return operation(data, length, out, func(payload []byte) (uint64, error) {
		return androidLibrary.registry.OpenResourceStream(uint64(engine), uint64(session), payload)
	})
}

//export muxvia_engine_send_resource_stream_frame
func muxvia_engine_send_resource_stream_frame(engine, stream C.muxvia_handle_t, data *C.uint8_t, length C.size_t) C.muxvia_status_v1 {
	if data == nil || length == 0 || uint64(length) > uint64(binding.MaxPayloadBytes) {
		return C.MUXVIA_STATUS_INVALID_ARGUMENT
	}
	payload := C.GoBytes(unsafe.Pointer(data), C.int(length))
	return status(androidLibrary.registry.SendResourceStreamFrame(uint64(engine), uint64(stream), payload))
}

//export muxvia_engine_close_resource_stream
func muxvia_engine_close_resource_stream(engine, stream C.muxvia_handle_t) C.muxvia_status_v1 {
	return status(androidLibrary.registry.CloseResourceStream(uint64(engine), uint64(stream)))
}

//export muxvia_engine_command
func muxvia_engine_command(engine C.muxvia_handle_t, data *C.uint8_t, length C.size_t, out *C.muxvia_handle_t) C.muxvia_status_v1 {
	return operation(data, length, out, func(payload []byte) (uint64, error) {
		return androidLibrary.registry.EngineCommand(uint64(engine), payload)
	})
}

//export muxvia_engine_next_event
func muxvia_engine_next_event(engine C.muxvia_handle_t, timeoutMillis C.uint32_t, out *C.muxvia_buffer_v1) C.muxvia_status_v1 {
	if out == nil {
		return C.MUXVIA_STATUS_INVALID_ARGUMENT
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

//export muxvia_platform_next_request
func muxvia_platform_next_request(engine C.muxvia_handle_t, timeoutMillis C.uint32_t, out *C.muxvia_buffer_v1) C.muxvia_status_v1 {
	if out == nil {
		return C.MUXVIA_STATUS_INVALID_ARGUMENT
	}
	androidLibrary.Lock()
	broker := androidLibrary.platforms[uint64(engine)]
	androidLibrary.Unlock()
	if broker == nil {
		return C.MUXVIA_STATUS_INVALID_HANDLE
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

//export muxvia_platform_complete
func muxvia_platform_complete(engine C.muxvia_handle_t, data *C.uint8_t, length C.size_t) C.muxvia_status_v1 {
	if data == nil || length == 0 || uint64(length) > uint64(binding.MaxPayloadBytes) {
		return C.MUXVIA_STATUS_INVALID_ARGUMENT
	}
	androidLibrary.Lock()
	broker := androidLibrary.platforms[uint64(engine)]
	androidLibrary.Unlock()
	if broker == nil {
		return C.MUXVIA_STATUS_INVALID_HANDLE
	}
	return status(broker.Complete(C.GoBytes(unsafe.Pointer(data), C.int(length))))
}

func allocateBuffer(payload []byte, out *C.muxvia_buffer_v1) C.muxvia_status_v1 {
	pointer := C.malloc(C.size_t(len(payload)))
	if pointer == nil {
		return C.MUXVIA_STATUS_CAPACITY
	}
	copy(unsafe.Slice((*byte)(pointer), len(payload)), payload)
	androidLibrary.Lock()
	androidLibrary.nextBuffer++
	handle := androidLibrary.nextBuffer
	if handle == 0 {
		androidLibrary.Unlock()
		C.free(pointer)
		return C.MUXVIA_STATUS_CAPACITY
	}
	androidLibrary.buffers[handle] = pointer
	androidLibrary.Unlock()
	out.buffer_handle = C.muxvia_handle_t(handle)
	out.data = (*C.uint8_t)(pointer)
	out.length = C.size_t(len(payload))
	return C.MUXVIA_STATUS_OK
}

//export muxvia_engine_cancel
func muxvia_engine_cancel(engine, operationHandle C.muxvia_handle_t) C.muxvia_status_v1 {
	return status(androidLibrary.registry.Cancel(uint64(engine), uint64(operationHandle)))
}

//export muxvia_engine_close_session
func muxvia_engine_close_session(engine, session C.muxvia_handle_t) C.muxvia_status_v1 {
	return status(androidLibrary.registry.CloseSession(uint64(engine), uint64(session)))
}

//export muxvia_engine_release
func muxvia_engine_release(engine, handle C.muxvia_handle_t) C.muxvia_status_v1 {
	return status(androidLibrary.registry.Release(uint64(engine), uint64(handle)))
}

//export muxvia_engine_close
func muxvia_engine_close(engine C.muxvia_handle_t) C.muxvia_status_v1 {
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

//export muxvia_buffer_free
func muxvia_buffer_free(buffer C.muxvia_handle_t) C.muxvia_status_v1 {
	handle := uint64(buffer)
	androidLibrary.Lock()
	pointer := androidLibrary.buffers[handle]
	if pointer != nil {
		delete(androidLibrary.buffers, handle)
	}
	androidLibrary.Unlock()
	if pointer == nil {
		return C.MUXVIA_STATUS_INVALID_HANDLE
	}
	C.free(pointer)
	return C.MUXVIA_STATUS_OK
}

func operation(data *C.uint8_t, length C.size_t, out *C.muxvia_handle_t, invoke func([]byte) (uint64, error)) C.muxvia_status_v1 {
	if data == nil || length == 0 || uint64(length) > uint64(binding.MaxPayloadBytes) || out == nil {
		return C.MUXVIA_STATUS_INVALID_ARGUMENT
	}
	payload := C.GoBytes(unsafe.Pointer(data), C.int(length))
	handle, err := invoke(payload)
	if err != nil {
		return status(err)
	}
	*out = C.muxvia_handle_t(handle)
	return C.MUXVIA_STATUS_OK
}

func status(err error) C.muxvia_status_v1 {
	if err == nil {
		return C.MUXVIA_STATUS_OK
	}
	switch {
	case errors.Is(err, binding.ErrInvalidHandle):
		return C.MUXVIA_STATUS_INVALID_HANDLE
	case errors.Is(err, binding.ErrClosed):
		return C.MUXVIA_STATUS_CLOSED
	case errors.Is(err, binding.ErrHandleActive):
		return C.MUXVIA_STATUS_INVALID_ARGUMENT
	default:
		return C.MUXVIA_STATUS_INTERNAL
	}
}

func main() {}
