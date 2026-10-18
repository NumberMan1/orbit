package util

import "github.com/NumberMan1/numbox/pkg/bitmap"

// Bitmap is the numbox owner-managed bitmap. It is not concurrent-safe.
type Bitmap = bitmap.Bitmap

func NewBitmap(size uint32) *Bitmap { return bitmap.NewBitmap(size) }
