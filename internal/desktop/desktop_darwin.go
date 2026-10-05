//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -fblocks
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void aq_configure(void);
void aq_show(void);
void aq_update(const char* content, const char* buttons);
*/
import "C"

import (
	"encoding/json"
	"unsafe"
)

type Button struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

var Events = make(chan int, 32)
var Workspaces = make(chan string, 8)

//export aq_action
func aq_action(id C.int) {
	select {
	case Events <- int(id):
	default:
	}
}

//export aq_workspace
func aq_workspace(path *C.char) {
	select {
	case Workspaces <- C.GoString(path):
	default:
	}
}
func Configure() { C.aq_configure() }
func Show()      { C.aq_show() }
func Update(content string, buttons []Button) {
	data, _ := json.Marshal(buttons)
	text := C.CString(content)
	actions := C.CString(string(data))
	defer C.free(unsafe.Pointer(text))
	defer C.free(unsafe.Pointer(actions))
	C.aq_update(text, actions)
}
