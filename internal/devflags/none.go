//go:build !cdp

// Package devflags holds switches that exist only in development builds.
package devflags

// BrowserArgs returns extra WebView2 arguments. Release builds add none.
func BrowserArgs() []string { return nil }
