//go:build windows && !smoke

package main

import (
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

func newDispatcher(warn func(string)) closer {
	return wire.Dispatcher(wire.Stores{}, warn)
}
