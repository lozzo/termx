package com.termx.app;

import android.os.Bundle;
import android.webkit.WebSettings;
import android.webkit.WebView;

import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {
    private static final String TAG = "TermxMainActivity";

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        TermxDebugLog.init(this);
        registerPlugin(NativeConnectionPlugin.class);
        registerPlugin(NativeFilePickerPlugin.class);
        registerPlugin(NativeHapticPlugin.class);
        super.onCreate(savedInstanceState);

        boolean isDebug = (getApplicationInfo().flags & android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE) != 0;
        WebView.setWebContentsDebuggingEnabled(isDebug);

        WebView webView = getBridge().getWebView();
        if (webView != null) {
            webView.setWebChromeClient(new TermxWebChromeClient(getBridge()));
            WebSettings settings = webView.getSettings();
            settings.setDomStorageEnabled(true);
            settings.setCacheMode(WebSettings.LOAD_DEFAULT);
            settings.setMixedContentMode(WebSettings.MIXED_CONTENT_ALWAYS_ALLOW);
        }
        TermxDebugLog.i(TAG, "MainActivity created debug=" + isDebug);
    }
}
