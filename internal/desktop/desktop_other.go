//go:build !darwin || !cgo

package desktop

type Button struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

var Events = make(chan int, 32)
var Workspaces = make(chan string, 8)

func Configure()              {}
func Show()                   {}
func Update(string, []Button) {}
