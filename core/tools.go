//go:build tools

// Keeps golang.org/x/mobile in go.mod: `gomobile bind` needs its bind
// package resolvable from this module.
package core

import _ "golang.org/x/mobile/bind"
