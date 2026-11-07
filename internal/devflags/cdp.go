//go:build cdp

// Package devflags holds switches that exist only in development builds.
package devflags

import (
	"os"
	"strconv"
)

// BrowserArgs opens the WebView2 DevTools protocol on BASALT_CDP_PORT so an
// end-to-end script can drive several app instances at once:
//
//	go build -tags cdp -o bin/basalt-cdp.exe .
//	set BASALT_CDP_PORT=9301 && bin\basalt-cdp.exe
//
// Never part of a release build: anything on the machine could control the
// app through that port.
func BrowserArgs() []string {
	p, err := strconv.Atoi(os.Getenv("BASALT_CDP_PORT"))
	if err != nil || p <= 1024 || p > 65535 {
		return nil
	}
	return []string{"--remote-debugging-port=" + strconv.Itoa(p)}
}
