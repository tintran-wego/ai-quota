//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -fblocks
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void aq_configure(void);
void aq_show(void);
void aq_update(const char* view);
*/
import "C"

import (
	"encoding/json"
	"unsafe"
)

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
func Update(view View) {
	data, _ := json.Marshal(view)
	text := C.CString(string(data))
	defer C.free(unsafe.Pointer(text))
	C.aq_update(text)
}
