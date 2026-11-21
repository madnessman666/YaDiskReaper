//go:build windows

package main

import _ "embed"

//go:embed aria2c_bin/windows/aria2c.exe
var embeddedAria []byte
