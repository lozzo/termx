package com.muxvia.app;

import android.webkit.ConsoleMessage;

import com.getcapacitor.Bridge;
import com.getcapacitor.BridgeWebChromeClient;

public class MuxviaWebChromeClient extends BridgeWebChromeClient {
    public MuxviaWebChromeClient(Bridge bridge) {
        super(bridge);
    }

    @Override
    public boolean onConsoleMessage(ConsoleMessage consoleMessage) {
        MuxviaDebugLog.console(consoleMessage);
        return super.onConsoleMessage(consoleMessage);
    }
}
