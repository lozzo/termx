#ifndef TERMX_CLIENT_H
#define TERMX_CLIENT_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#define TERMX_CLIENT_ABI_VERSION 2u

typedef uint64_t termx_handle_t;

typedef enum termx_status_v1 {
  TERMX_STATUS_OK = 0,
  TERMX_STATUS_INVALID_ARGUMENT = 1,
  TERMX_STATUS_INVALID_HANDLE = 2,
  TERMX_STATUS_CLOSED = 3,
  TERMX_STATUS_CAPACITY = 4,
  TERMX_STATUS_INTERNAL = 5
} termx_status_v1;

/* Inputs are borrowed only for the duration of the call. Event output is
 * wrapper-owned memory identified by buffer_handle and must be released once
 * with termx_buffer_free. No returned data pointer may reference Go memory. */
typedef struct termx_buffer_v1 {
  termx_handle_t buffer_handle;
  const uint8_t *data;
  size_t length;
} termx_buffer_v1;

uint32_t termx_client_abi_version(void);
termx_status_v1 termx_engine_create(termx_handle_t *out_engine_handle);
termx_status_v1 termx_engine_open_session(termx_handle_t engine_handle, const uint8_t *request_proto, size_t request_length, termx_handle_t *out_operation_handle);
termx_status_v1 termx_engine_execute(termx_handle_t engine_handle, termx_handle_t session_handle, const uint8_t *command_proto, size_t command_length, termx_handle_t *out_operation_handle);
termx_status_v1 termx_engine_open_resource_stream(termx_handle_t engine_handle, termx_handle_t session_handle, const uint8_t *request_proto, size_t request_length, termx_handle_t *out_stream_handle);
termx_status_v1 termx_engine_send_resource_stream_frame(termx_handle_t engine_handle, termx_handle_t stream_handle, const uint8_t *frame_proto, size_t frame_length);
termx_status_v1 termx_engine_close_resource_stream(termx_handle_t engine_handle, termx_handle_t stream_handle);
termx_status_v1 termx_engine_import_pairing(termx_handle_t engine_handle, const uint8_t *request_proto, size_t request_length, termx_handle_t *out_operation_handle);
termx_status_v1 termx_engine_delete_credential(termx_handle_t engine_handle, const uint8_t *request_proto, size_t request_length, termx_handle_t *out_operation_handle);
termx_status_v1 termx_engine_next_event(termx_handle_t engine_handle, uint32_t timeout_millis, termx_buffer_v1 *out_event_proto);
termx_status_v1 termx_platform_next_request(termx_handle_t engine_handle, uint32_t timeout_millis, termx_buffer_v1 *out_request_proto);
termx_status_v1 termx_platform_complete(termx_handle_t engine_handle, const uint8_t *response_proto, size_t response_length);
termx_status_v1 termx_engine_cancel(termx_handle_t engine_handle, termx_handle_t operation_handle);
termx_status_v1 termx_engine_close_session(termx_handle_t engine_handle, termx_handle_t session_handle);
termx_status_v1 termx_engine_release(termx_handle_t engine_handle, termx_handle_t handle);
termx_status_v1 termx_engine_close(termx_handle_t engine_handle);
termx_status_v1 termx_buffer_free(termx_handle_t buffer_handle);

#ifdef __cplusplus
}
#endif

#endif
