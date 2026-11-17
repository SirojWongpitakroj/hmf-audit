package diskindex

import (
	"blas/bhash"
	"blas/bitmap"
	"context"
	"fmt"
)

// PreparedKeyword and PreparedBitmap expose no mutable bytes. Copies share only
// immutable, privately owned encodings; their constructors derive every hash.
type PreparedKeyword struct {
	word   string
	framed []byte
}
type PreparedBitmap struct {
	n     uint64
	data  []byte
	inner bhash.Digest
	valid bool
}

func PrepareKeyword(word string) PreparedKeyword {
	return PreparedKeyword{word, bhash.FramedString(word)}
}
func PrepareBitmap(bm bitmap.Bitmap) PreparedBitmap {
	data := bm.Bytes()
	return PreparedBitmap{bm.Len(), data, bhash.H(data), true}
}
func (p PreparedBitmap) Len() uint64 { return p.n }

func (b *Builder) AddPreparedKeywordBitmap(ctx context.Context, key PreparedKeyword, bm PreparedBitmap) error {
	if b.Meta.Kind != "iimt" || !bm.valid || len(key.framed) == 0 || bm.n != b.Meta.N {
		return fmt.Errorf("diskindex: prepared bitmap kind/population differs or input is uninitialized")
	}
	hash := bhash.HashConcat(key.framed, bm.inner[:])
	return b.addBitmap(ctx, key.word, 0, bm.data, hash)
}
