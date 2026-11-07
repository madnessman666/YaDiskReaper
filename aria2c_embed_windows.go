//go:build windows

package main

import _ "embed"

//go:embed aria2c_bin/aria2c.exe
var embeddedAria []byte
