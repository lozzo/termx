# Official Android Cloud Module

This private source set is added only by the official APK build:

```bash
cd clients/mobile/android
./gradlew -I ../../../private/cloud/mobile/android/official-cloud.init.gradle testDebugUnitTest assembleDebug
```

Community builds do not reference this directory and resolve the fixed factory class to the disabled public adapter. The private module implements only `ManagedCloudAdapter`; WebRTC, grant storage, DeviceIdentity/capability authorization, DataChannel, and terminal protocol remain in the public App layer.

The development gateway intentionally returns `login_required` until the official OAuth/TLS mobile SDK is injected. It does not restore the archived Hub/session-token protocol.
