#ifndef MUXVIA_CLIENT_H
#define MUXVIA_CLIENT_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#define MUXVIA_CLIENT_ABI_VERSION 3u

typedef uint64_t muxvia_handle_t;

typedef enum muxvia_status_v1 {
  MUXVIA_STATUS_OK = 0,
  MUXVIA_STATUS_INVALID_ARGUMENT = 1,
  MUXVIA_STATUS_INVALID_HANDLE = 2,
  MUXVIA_STATUS_CLOSED = 3,
  MUXVIA_STATUS_CAPACITY = 4,
  MUXVIA_STATUS_INTERNAL = 5
} muxvia_status_v1;

/* Inputs are borrowed only for the duration of the call. Event output is
 * wrapper-owned memory identified by buffer_handle and must be released once
 * with muxvia_buffer_free. No returned data pointer may reference Go memory. */
typedef struct muxvia_buffer_v1 {
  muxvia_handle_t buffer_handle;
  const uint8_t *data;
  size_t length;
} muxvia_buffer_v1;

uint32_t muxvia_client_abi_version(void);
muxvia_status_v1 muxvia_engine_create(muxvia_handle_t *out_engine_handle);
muxvia_status_v1 muxvia_engine_open_session(muxvia_handle_t engine_handle, const uint8_t *request_proto, size_t request_length, muxvia_handle_t *out_operation_handle);
muxvia_status_v1 muxvia_engine_execute(muxvia_handle_t engine_handle, muxvia_handle_t session_handle, const uint8_t *command_proto, size_t command_length, muxvia_handle_t *out_operation_handle);
muxvia_status_v1 muxvia_engine_open_resource_stream(muxvia_handle_t engine_handle, muxvia_handle_t session_handle, const uint8_t *request_proto, size_t request_length, muxvia_handle_t *out_stream_handle);
muxvia_status_v1 muxvia_engine_send_resource_stream_frame(muxvia_handle_t engine_handle, muxvia_handle_t stream_handle, const uint8_t *frame_proto, size_t frame_length);
muxvia_status_v1 muxvia_engine_close_resource_stream(muxvia_handle_t engine_handle, muxvia_handle_t stream_handle);
muxvia_status_v1 muxvia_engine_command(muxvia_handle_t engine_handle, const uint8_t *command_proto, size_t command_length, muxvia_handle_t *out_operation_handle);
muxvia_status_v1 muxvia_engine_next_event(muxvia_handle_t engine_handle, uint32_t timeout_millis, muxvia_buffer_v1 *out_event_proto);
muxvia_status_v1 muxvia_platform_next_request(muxvia_handle_t engine_handle, uint32_t timeout_millis, muxvia_buffer_v1 *out_request_proto);
muxvia_status_v1 muxvia_platform_complete(muxvia_handle_t engine_handle, const uint8_t *response_proto, size_t response_length);
muxvia_status_v1 muxvia_engine_cancel(muxvia_handle_t engine_handle, muxvia_handle_t operation_handle);
muxvia_status_v1 muxvia_engine_close_session(muxvia_handle_t engine_handle, muxvia_handle_t session_handle);
muxvia_status_v1 muxvia_engine_release(muxvia_handle_t engine_handle, muxvia_handle_t handle);
muxvia_status_v1 muxvia_engine_close(muxvia_handle_t engine_handle);
muxvia_status_v1 muxvia_buffer_free(muxvia_handle_t buffer_handle);

#ifdef __cplusplus
}
#endif

#endif
