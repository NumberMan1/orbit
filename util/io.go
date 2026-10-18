package util

import (
	"github.com/NumberMan1/numbox/utils/ioutil"
	"io"
)

// WriteFull delegates short-write and progress checks to numbox.
func WriteFull(writer io.Writer, data []byte) error { return ioutil.WriteFull(writer, data) }
