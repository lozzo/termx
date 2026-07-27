package com.anytty.app;

import android.os.Bundle;
import android.webkit.WebSettings;
import android.webkit.WebView;

import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {
    private static final String TAG = "AnyTTYMainActivity";

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        AnyTTYDebugLog.init(this);
        registerPlugin(NativeConnectionPlugin.class);
        registerPlugin(NativeFilePickerPlugin.class);
        registerPlugin(NativeHapticPlugin.class);
        super.onCreate(savedInstanceState);

        boolean isDebug = (getApplicationInfo().flags & android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE) != 0;
        WebView.setWebContentsDebuggingEnabled(isDebug);

        WebView webView = getBridge().getWebView();
        if (webView != null) {
            webView.setWebChromeClient(new AnyTTYWebChromeClient(getBridge()));
            webView.setOverScrollMode(WebView.OVER_SCROLL_NEVER);
            webView.setVerticalScrollBarEnabled(false);
            webView.setHorizontalScrollBarEnabled(false);
            WebSettings settings = webView.getSettings();
            settings.setDomStorageEnabled(true);
            settings.setCacheMode(WebSettings.LOAD_DEFAULT);
            settings.setMixedContentMode(WebSettings.MIXED_CONTENT_ALWAYS_ALLOW);
        }
        AnyTTYDebugLog.i(TAG, "MainActivity created debug=" + isDebug);
    }
}
