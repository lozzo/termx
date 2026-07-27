package com.anytty.app;

import android.webkit.ConsoleMessage;

import com.getcapacitor.Bridge;
import com.getcapacitor.BridgeWebChromeClient;

public class AnyTTYWebChromeClient extends BridgeWebChromeClient {
    public AnyTTYWebChromeClient(Bridge bridge) {
        super(bridge);
    }

    @Override
    public boolean onConsoleMessage(ConsoleMessage consoleMessage) {
        AnyTTYDebugLog.console(consoleMessage);
        return super.onConsoleMessage(consoleMessage);
    }
}
