//go:build windows && !smoke

package main

import (
	"context"
	"net/http"

	"github.com/erkanrzgc/kadran/desktop/internal/bridge"
	"github.com/erkanrzgc/kadran/desktop/internal/wire"
)

// smokeBuild: dağıtılan ikilide sahte dağıtıcı ve onay atlatması YOK.
const smokeBuild = false

// closer, kapanırken açık SSH bağlantılarını kapatan dağıtıcı.
type closer interface {
	bridge.Dispatcher
	Close()
}

// smokeUserData: dağıtılan ikilide hep kullanıcının kendi dizini.
func smokeUserData() string { return "" }

func newDispatcher(warn func(string)) closer {
	return wire.Dispatcher(wire.Stores{}, warn)
}

// smokeMiddleware ve startSmoke yalnız duman ikilisinde iş görür (smoke_windows.go).
func smokeMiddleware(next http.Handler) http.Handler { return next }

func startSmoke(context.Context, *host) {}
