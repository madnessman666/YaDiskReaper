//go:build linux

package main

import _ "embed"

//go:embed aria2c_bin/aria2c
var embeddedAria []byte
