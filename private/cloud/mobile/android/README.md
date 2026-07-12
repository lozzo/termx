# Official Android Cloud Module

This private source set is added only by the Official APK build. The normal Official build keeps cloud login fail closed:

```bash
cd clients/mobile/android
./gradlew -I ../../../private/cloud/mobile/android/official-cloud.init.gradle testDebugUnitTest assembleDebug
```

The explicit local development build enables the loopback Control Plane and Hub adapter used with `adb reverse`:

```bash
cd clients/mobile/android
./gradlew \
  -I ../../../private/cloud/mobile/android/official-cloud.init.gradle \
  -PtermxOfficialDevCloud=true \
  testDebugUnitTest assembleDebug
```

This flag fixes the Android-side origins to `http://127.0.0.1:41001` and `http://127.0.0.1:41002`. It is not enabled by default and accepts only loopback HTTP. The gateway uses the same serialized dev Control Plane and Hub contract as the desktop Companion, including account session, endpoint resolve, Hub admission, signaling stream, and explicit Relay lease errors.

The explicit public HTTP staging build targets the fixed CLOUD008 test server without `adb reverse`:

```bash
./gradlew \
  -I ../../../private/cloud/mobile/android/official-cloud.init.gradle \
  -PtermxOfficialPublicHTTPStaging=true \
  testDebugUnitTest assembleDebug
```

This profile is mutually exclusive with `termxOfficialDevCloud`, carries the fixed test account session over cleartext HTTP, and must never be used for production accounts or data.

Community builds do not reference this directory and resolve the fixed factory class to the disabled public adapter. The private module implements only `ManagedCloudAdapter`; WebRTC, real DTLS certificate binding, grant storage, capability authorization, the single `protocol` DataChannel, and terminal protocol remain in the public App layer.

Run the complete Community/Official build boundary from the repository root with `make test-android`. The ADB setup, daemon preparation, pairing, terminal input, background recovery, and failure checks are documented in [`docs/remote-platform/android-devcloud-manual-test.md`](../../../../docs/remote-platform/android-devcloud-manual-test.md).

Production OAuth/TLS remains deferred. No build restores the archived Web Controller, old Hub session-token protocol, or legacy api/events/terminal/file DataChannels.
