//go:build darwin

package main

import _ "embed"

//go:embed aria2c_bin/aria2c_darwin
var embeddedAria []byte