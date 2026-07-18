#include <jni.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#include "termx_client.h"

extern termx_status_v1 termx_android_spike_set_runtime_dir(const char *runtime_dir);
extern termx_status_v1 termx_android_spike_engine_create(termx_handle_t *out_engine_handle);

static int throw_status(JNIEnv *env, termx_status_v1 status) {
  if (status == TERMX_STATUS_OK) {
    return 0;
  }
  jclass exception = (*env)->FindClass(env, "java/lang/IllegalStateException");
  if (exception != NULL) {
    char message[64];
    snprintf(message, sizeof(message), "termx native status %d", (int)status);
    (*env)->ThrowNew(env, exception, message);
  }
  return -1;
}

static jbyte *borrow_payload(JNIEnv *env, jbyteArray payload, jsize *length) {
  if (payload == NULL) {
    throw_status(env, TERMX_STATUS_INVALID_ARGUMENT);
    return NULL;
  }
  *length = (*env)->GetArrayLength(env, payload);
  return (*env)->GetByteArrayElements(env, payload, NULL);
}

JNIEXPORT jint JNICALL
Java_com_termx_app_goclient_GoClientNative_abiVersion(JNIEnv *env, jobject self) {
  (void)env;
  (void)self;
  return (jint)termx_client_abi_version();
}

JNIEXPORT jlong JNICALL
Java_com_termx_app_goclient_GoClientNative_create(JNIEnv *env, jobject self) {
  (void)self;
  termx_handle_t engine = 0;
  if (throw_status(env, termx_engine_create(&engine)) != 0) {
    return 0;
  }
  return (jlong)engine;
}

JNIEXPORT jlong JNICALL
Java_com_termx_app_goclient_GoClientNative_createSpike(JNIEnv *env, jobject self, jstring runtime_dir) {
  (void)self;
  if (runtime_dir == NULL) {
    throw_status(env, TERMX_STATUS_INVALID_ARGUMENT);
    return 0;
  }
  const char *path = (*env)->GetStringUTFChars(env, runtime_dir, NULL);
  if (path == NULL) {
    return 0;
  }
  termx_status_v1 status = termx_android_spike_set_runtime_dir(path);
  (*env)->ReleaseStringUTFChars(env, runtime_dir, path);
  if (throw_status(env, status) != 0) {
    return 0;
  }
  termx_handle_t engine = 0;
  if (throw_status(env, termx_android_spike_engine_create(&engine)) != 0) {
    return 0;
  }
  return (jlong)engine;
}

JNIEXPORT jlong JNICALL
Java_com_termx_app_goclient_GoClientNative_openSession(JNIEnv *env, jobject self, jlong engine, jbyteArray payload) {
  (void)self;
  jsize length = 0;
  jbyte *bytes = borrow_payload(env, payload, &length);
  if (bytes == NULL) {
    return 0;
  }
  termx_handle_t operation = 0;
  termx_status_v1 status = termx_engine_open_session(
      (termx_handle_t)engine, (const uint8_t *)bytes, (size_t)length, &operation);
  (*env)->ReleaseByteArrayElements(env, payload, bytes, JNI_ABORT);
  return throw_status(env, status) == 0 ? (jlong)operation : 0;
}

JNIEXPORT jlong JNICALL
Java_com_termx_app_goclient_GoClientNative_execute(JNIEnv *env, jobject self, jlong engine, jlong session, jbyteArray payload) {
  (void)self;
  jsize length = 0;
  jbyte *bytes = borrow_payload(env, payload, &length);
  if (bytes == NULL) {
    return 0;
  }
  termx_handle_t operation = 0;
  termx_status_v1 status = termx_engine_execute(
      (termx_handle_t)engine, (termx_handle_t)session,
      (const uint8_t *)bytes, (size_t)length, &operation);
  (*env)->ReleaseByteArrayElements(env, payload, bytes, JNI_ABORT);
  return throw_status(env, status) == 0 ? (jlong)operation : 0;
}

JNIEXPORT jlong JNICALL
Java_com_termx_app_goclient_GoClientNative_openResourceStream(JNIEnv *env, jobject self, jlong engine, jlong session, jbyteArray payload) {
  (void)self;
  jsize length = 0;
  jbyte *bytes = borrow_payload(env, payload, &length);
  if (bytes == NULL) {
    return 0;
  }
  termx_handle_t stream = 0;
  termx_status_v1 status = termx_engine_open_resource_stream(
      (termx_handle_t)engine, (termx_handle_t)session,
      (const uint8_t *)bytes, (size_t)length, &stream);
  (*env)->ReleaseByteArrayElements(env, payload, bytes, JNI_ABORT);
  return throw_status(env, status) == 0 ? (jlong)stream : 0;
}

JNIEXPORT void JNICALL
Java_com_termx_app_goclient_GoClientNative_sendResourceStreamFrame(JNIEnv *env, jobject self, jlong engine, jlong stream, jbyteArray payload) {
  (void)self;
  jsize length = 0;
  jbyte *bytes = borrow_payload(env, payload, &length);
  if (bytes == NULL) {
    return;
  }
  termx_status_v1 status = termx_engine_send_resource_stream_frame(
      (termx_handle_t)engine, (termx_handle_t)stream,
      (const uint8_t *)bytes, (size_t)length);
  (*env)->ReleaseByteArrayElements(env, payload, bytes, JNI_ABORT);
  throw_status(env, status);
}

JNIEXPORT void JNICALL
Java_com_termx_app_goclient_GoClientNative_closeResourceStream(JNIEnv *env, jobject self, jlong engine, jlong stream) {
  (void)self;
  throw_status(env, termx_engine_close_resource_stream((termx_handle_t)engine, (termx_handle_t)stream));
}

JNIEXPORT jlong JNICALL
Java_com_termx_app_goclient_GoClientNative_engineCommand(JNIEnv *env, jobject self, jlong engine, jbyteArray payload) {
  (void)self;
  jsize length = 0;
  jbyte *bytes = borrow_payload(env, payload, &length);
  if (bytes == NULL) {
    return 0;
  }
  termx_handle_t operation = 0;
  termx_status_v1 status = termx_engine_command(
      (termx_handle_t)engine, (const uint8_t *)bytes, (size_t)length, &operation);
  (*env)->ReleaseByteArrayElements(env, payload, bytes, JNI_ABORT);
  return throw_status(env, status) == 0 ? (jlong)operation : 0;
}

JNIEXPORT jbyteArray JNICALL
Java_com_termx_app_goclient_GoClientNative_nextEvent(JNIEnv *env, jobject self, jlong engine, jint timeout_millis) {
  (void)self;
  termx_buffer_v1 event = {0};
  if (throw_status(env, termx_engine_next_event((termx_handle_t)engine, (uint32_t)timeout_millis, &event)) != 0) {
    return NULL;
  }
  jbyteArray result = (*env)->NewByteArray(env, (jsize)event.length);
  if (result != NULL && event.length > 0) {
    (*env)->SetByteArrayRegion(env, result, 0, (jsize)event.length, (const jbyte *)event.data);
  }
  termx_status_v1 free_status = termx_buffer_free(event.buffer_handle);
  if (result != NULL && throw_status(env, free_status) != 0) {
    return NULL;
  }
  return result;
}

JNIEXPORT jbyteArray JNICALL
Java_com_termx_app_goclient_GoClientNative_nextPlatformRequest(JNIEnv *env, jobject self, jlong engine, jint timeout_millis) {
  (void)self;
  termx_buffer_v1 request = {0};
  if (throw_status(env, termx_platform_next_request((termx_handle_t)engine, (uint32_t)timeout_millis, &request)) != 0) {
    return NULL;
  }
  jbyteArray result = (*env)->NewByteArray(env, (jsize)request.length);
  if (result != NULL && request.length > 0) {
    (*env)->SetByteArrayRegion(env, result, 0, (jsize)request.length, (const jbyte *)request.data);
  }
  termx_status_v1 free_status = termx_buffer_free(request.buffer_handle);
  if (result != NULL && throw_status(env, free_status) != 0) {
    return NULL;
  }
  return result;
}

JNIEXPORT void JNICALL
Java_com_termx_app_goclient_GoClientNative_completePlatformRequest(JNIEnv *env, jobject self, jlong engine, jbyteArray payload) {
  (void)self;
  jsize length = 0;
  jbyte *bytes = borrow_payload(env, payload, &length);
  if (bytes == NULL) {
    return;
  }
  termx_status_v1 status = termx_platform_complete(
      (termx_handle_t)engine, (const uint8_t *)bytes, (size_t)length);
  (*env)->ReleaseByteArrayElements(env, payload, bytes, JNI_ABORT);
  throw_status(env, status);
}

JNIEXPORT void JNICALL
Java_com_termx_app_goclient_GoClientNative_cancel(JNIEnv *env, jobject self, jlong engine, jlong operation) {
  (void)self;
  throw_status(env, termx_engine_cancel((termx_handle_t)engine, (termx_handle_t)operation));
}

JNIEXPORT void JNICALL
Java_com_termx_app_goclient_GoClientNative_closeSession(JNIEnv *env, jobject self, jlong engine, jlong session) {
  (void)self;
  throw_status(env, termx_engine_close_session((termx_handle_t)engine, (termx_handle_t)session));
}

JNIEXPORT void JNICALL
Java_com_termx_app_goclient_GoClientNative_release(JNIEnv *env, jobject self, jlong engine, jlong handle) {
  (void)self;
  throw_status(env, termx_engine_release((termx_handle_t)engine, (termx_handle_t)handle));
}

JNIEXPORT void JNICALL
Java_com_termx_app_goclient_GoClientNative_close(JNIEnv *env, jobject self, jlong engine) {
  (void)self;
  throw_status(env, termx_engine_close((termx_handle_t)engine));
}
