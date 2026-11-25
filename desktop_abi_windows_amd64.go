//go:build windows && amd64

package main

import "unsafe"

var _ [80]byte = [unsafe.Sizeof(windowClass{})]byte{}
var _ [72]byte = [unsafe.Sizeof(paintStruct{})]byte{}
var _ [48]byte = [unsafe.Sizeof(message{})]byte{}
var _ [976]byte = [unsafe.Sizeof(notifyIcon{})]byte{}
