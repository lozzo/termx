package com.termx.app;

import android.webkit.ConsoleMessage;

import com.getcapacitor.Bridge;
import com.getcapacitor.BridgeWebChromeClient;

public class TermxWebChromeClient extends BridgeWebChromeClient {
    public TermxWebChromeClient(Bridge bridge) {
        super(bridge);
    }

    @Override
    public boolean onConsoleMessage(ConsoleMessage consoleMessage) {
        TermxDebugLog.console(consoleMessage);
        return super.onConsoleMessage(consoleMessage);
    }
}
