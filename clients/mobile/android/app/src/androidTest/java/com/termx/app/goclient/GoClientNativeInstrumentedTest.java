package com.termx.app.goclient;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertTrue;

import androidx.test.core.app.ApplicationProvider;
import androidx.test.ext.junit.runners.AndroidJUnit4;

import com.termx.app.goclient.GoClientNative;

import org.junit.Test;
import org.junit.runner.RunWith;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;

import termx.api.v1.Application;
import termx.api.v1.Common;
import termx.api.v1.Events;
import termx.api.v1.Storage;
import termx.client.binding.v1.ClientBinding;

/** GoClientNativeInstrumentedTest 在 APK 进程中证明 Go/Pion/auth/Hello/API/event/cancel/close 纵向链路。 */
@RunWith(AndroidJUnit4.class)
public final class GoClientNativeInstrumentedTest {
    @Test
    public void loadsAndRunsRealGoClientEngine() throws Exception {
        assertEquals(2, GoClientNative.INSTANCE.abiVersion());
        long engine = GoClientNative.INSTANCE.createSpike(
                ApplicationProvider.getApplicationContext().getCacheDir().getAbsolutePath());
        assertNotEquals(0, engine);

        long openOperation = GoClientNative.INSTANCE.openSession(engine,
                ClientBinding.OpenSessionRequest.newBuilder()
                        .setRequestId("android-open")
                        .setEndpointId("android-spike")
                        .setIntent(ClientBinding.ConnectIntent.CONNECT_INTENT_INTERACTIVE)
                        .build().toByteArray());
        ClientBinding.EventEnvelope openEvent = next(engine);
        assertTrue(openEvent.hasOpenSession());
        assertEquals(openOperation, openEvent.getOpenSession().getOperationHandle());
        long session = openEvent.getOpenSession().getSessionHandle();
        assertNotEquals(0, session);
        GoClientNative.INSTANCE.release(engine, openOperation);
        Common.EndpointSessionStamp sessionStamp = openEvent.getOpenSession().getSession();

        long subscribeOperation = GoClientNative.INSTANCE.execute(engine, session,
                Application.CommandEnvelope.newBuilder()
                        .setContext(requestContext("android-subscribe", sessionStamp))
                        .setEventSubscribe(Events.EventSubscribeCommand.newBuilder()
                                .addTypes(Events.ApplicationEventType.APPLICATION_EVENT_TYPE_STORAGE_CHANGED)
                                .setStorageAppId("android-spike")
                                .setStorageScope(Storage.StorageScope.STORAGE_SCOPE_PUBLIC))
                        .build().toByteArray());
        ClientBinding.EventEnvelope subscribeEvent = next(engine);
        assertTrue(subscribeEvent.toString(), subscribeEvent.hasExecute());
        assertTrue(subscribeEvent.toString(),
                subscribeEvent.getExecute().getResult().hasEventSubscription());
        GoClientNative.INSTANCE.release(engine, subscribeOperation);

        Storage.StorageKey key = Storage.StorageKey.newBuilder()
                .setAppId("android-spike")
                .setScope(Storage.StorageScope.STORAGE_SCOPE_PUBLIC)
                .setKey("jni-proof")
                .build();
        long putOperation = GoClientNative.INSTANCE.execute(engine, session,
                Application.CommandEnvelope.newBuilder()
                        .setContext(requestContext("android-put", sessionStamp))
                        .setStoragePut(Storage.StoragePutCommand.newBuilder()
                                .setKey(key)
                                .setValue(com.google.protobuf.ByteString.copyFromUtf8("ok")))
                        .build().toByteArray());
        boolean sawPut = false;
        boolean sawStorageEvent = false;
        for (int i = 0; i < 4 && (!sawPut || !sawStorageEvent); i++) {
            ClientBinding.EventEnvelope event = next(engine);
            if (event.hasExecute() && event.getExecute().getOperationHandle() == putOperation) {
                sawPut = event.getExecute().getResult().hasStoragePut();
            }
            if (event.hasApplication()) {
                sawStorageEvent = event.getApplication().getEvent().hasStorageChanged();
            }
        }
        assertTrue(sawPut);
        assertTrue(sawStorageEvent);
        GoClientNative.INSTANCE.release(engine, putOperation);

        long cancelOperation = GoClientNative.INSTANCE.openSession(engine,
                ClientBinding.OpenSessionRequest.newBuilder()
                        .setRequestId("android-cancel")
                        .setEndpointId("cancel")
                        .setIntent(ClientBinding.ConnectIntent.CONNECT_INTENT_PROBE)
                        .build().toByteArray());
        GoClientNative.INSTANCE.cancel(engine, cancelOperation);
        ClientBinding.EventEnvelope cancelEvent = next(engine);
        assertTrue(cancelEvent.hasOpenSession());
        assertEquals(Common.ApiErrorCode.API_ERROR_CODE_CANCELLED,
                cancelEvent.getOpenSession().getError().getCode());
        GoClientNative.INSTANCE.release(engine, cancelOperation);

        GoClientNative.INSTANCE.closeSession(engine, session);
        GoClientNative.INSTANCE.closeSession(engine, session);
        ClientBinding.EventEnvelope closed = next(engine);
        assertTrue(closed.hasSessionClosed());
        GoClientNative.INSTANCE.release(engine, session);
        GoClientNative.INSTANCE.close(engine);
    }

    @Test
    public void acceptsCallsFromIndependentJvmThreads() throws Exception {
        List<Callable<Integer>> calls = new ArrayList<>();
        for (int i = 0; i < 16; i++) {
            calls.add(() -> GoClientNative.INSTANCE.abiVersion());
        }
        ExecutorService executor = Executors.newFixedThreadPool(4);
        try {
            for (Future<Integer> result : executor.invokeAll(calls)) {
                assertEquals(2, result.get().intValue());
            }
        } finally {
            executor.shutdownNow();
        }
    }

    @Test
    public void productionEngineExchangesPlatformProtoRequests() throws Exception {
        long engine = GoClientNative.INSTANCE.create();
        try {
            long operation = GoClientNative.INSTANCE.deleteCredential(engine,
                    ClientBinding.DeleteCredentialRequest.newBuilder()
                            .setRequestId("delete-platform")
                            .setCredentialRef("credential:studio")
                            .build().toByteArray());
            ClientBinding.PlatformRequest request = ClientBinding.PlatformRequest.parseFrom(
                    GoClientNative.INSTANCE.nextPlatformRequest(engine, 5_000));
            assertTrue(request.hasCredentialDelete());
            assertEquals("credential:studio", request.getCredentialDelete().getCredentialRef());
            GoClientNative.INSTANCE.completePlatformRequest(engine,
                    ClientBinding.PlatformResponse.newBuilder()
                            .setRequestId(request.getRequestId())
                            .build().toByteArray());
            ClientBinding.EventEnvelope event = next(engine);
            assertTrue(event.hasDeleteCredential());
            assertEquals(operation, event.getDeleteCredential().getOperationHandle());
            GoClientNative.INSTANCE.release(engine, operation);
        } finally {
            GoClientNative.INSTANCE.close(engine);
        }
    }

    @Test
    public void screenFreezeTeardownInvalidatesHandlesAndAdvancesGeneration() throws Exception {
        String runtimeDir = ApplicationProvider.getApplicationContext().getCacheDir().getAbsolutePath();
        long firstEngine = GoClientNative.INSTANCE.createSpike(runtimeDir);
        Common.EndpointSessionStamp firstStamp;
        try {
            GoClientNative.INSTANCE.openSession(firstEngine,
                    ClientBinding.OpenSessionRequest.newBuilder()
                            .setRequestId("before-freeze")
                            .setEndpointId("android-spike")
                            .setIntent(ClientBinding.ConnectIntent.CONNECT_INTENT_INTERACTIVE)
                            .build().toByteArray());
            firstStamp = next(firstEngine).getOpenSession().getSession();
        } finally {
            GoClientNative.INSTANCE.close(firstEngine);
        }
        boolean staleRejected = false;
        try {
            GoClientNative.INSTANCE.nextEvent(firstEngine, 1);
        } catch (IllegalStateException expected) {
            staleRejected = true;
        }
        assertTrue("closed engine handle must be rejected after WebView freeze", staleRejected);

        long secondEngine = GoClientNative.INSTANCE.createSpike(runtimeDir);
        try {
            GoClientNative.INSTANCE.openSession(secondEngine,
                    ClientBinding.OpenSessionRequest.newBuilder()
                            .setRequestId("after-resume")
                            .setEndpointId("android-spike")
                            .setIntent(ClientBinding.ConnectIntent.CONNECT_INTENT_INTERACTIVE)
                            .build().toByteArray());
            Common.EndpointSessionStamp secondStamp = next(secondEngine).getOpenSession().getSession();
            assertTrue("resumed engine must allocate a newer process generation",
                    secondStamp.getGeneration() > firstStamp.getGeneration());
        } finally {
            GoClientNative.INSTANCE.close(secondEngine);
        }
    }

    private static ClientBinding.EventEnvelope next(long engine) throws Exception {
        return ClientBinding.EventEnvelope.parseFrom(GoClientNative.INSTANCE.nextEvent(engine, 15_000));
    }

    private static Common.RequestContext requestContext(
            String requestId, Common.EndpointSessionStamp session) {
        return Common.RequestContext.newBuilder()
                .setRequestId(requestId)
                .setApiVersion(Common.ApiVersion.newBuilder().setMajor(1))
                .setSession(session)
                .build();
    }
}
