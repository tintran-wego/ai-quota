//go:build !darwin || !cgo

package desktop

var Events = make(chan int, 32)
var Workspaces = make(chan string, 8)

func Configure()  {}
func Show()       {}
func Update(View) {}
