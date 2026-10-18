//go:build !linux && !darwin

package server

import (
	"errors"
	"net"
	"os"
	"reflect"
)

// socketPeerUID is not available here; the peer check is installed only
// on Linux and Mac.
func socketPeerUID(*net.UnixConn) (int, error) {
	return -1, errors.ErrUnsupported
}

// fileOwner reports the user that owns the file info describes, from the
// Uid field of the platform's stat record (the BSDs have one; Windows does
// not, so there the owner cannot be checked).
func fileOwner(info os.FileInfo) (int, bool) {
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return -1, false
	}
	uid := v.FieldByName("Uid")
	if !uid.IsValid() || !uid.CanUint() {
		return -1, false
	}
	return int(uid.Uint()), true
}
