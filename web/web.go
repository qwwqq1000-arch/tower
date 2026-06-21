// Package web embeds the minimal Tower admin console.
package web

import _ "embed"

//go:embed index.html
var IndexHTML []byte
