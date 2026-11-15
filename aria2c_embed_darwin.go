//go:build darwin

package main

import _ "embed"

//go:embed aria2c_bin/darwin/aria2c
var embeddedAria []byte