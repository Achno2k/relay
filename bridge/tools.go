//go:build tools

// Package relay pins dependencies that the packages below adopt as they land, so go.mod stays
// owned by go-core and `go mod tidy` keeps them.
package relay

import (
	_ "github.com/coder/websocket"
	_ "github.com/fsnotify/fsnotify"
	_ "rsc.io/qr"
)
