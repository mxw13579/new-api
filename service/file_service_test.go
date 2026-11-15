package service

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func heifTestBox(boxType string, payload ...[]byte) []byte {
	body := make([]byte, 0)
	for _, p := range payload {
		body = append(body, p...)
	}
	box := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	box = append(box, boxType...)
	return append(box, body...)
}

func heifTestISPE(width, height uint32) []byte {
	content := []byte{0, 0, 0, 0} // version/flags
	content = binary.BigEndian.AppendUint32(content, width)
	content = binary.BigEndian.AppendUint32(content, height)
	return heifTestBox("ispe", content)
}

func heifTestFile(metaChildren ...[]byte) []byte {
	ftyp := heifTestBox("ftyp", []byte("heic"), []byte{0, 0, 0, 0}, []byte("mif1heic"))
	meta := heifTestBox("meta", append([][]byte{{0, 0, 0, 0}}, metaChildren...)...)
	return append(ftyp, meta...)
}

func TestParseHEIFDimensions(t *testing.T) {
	nested := heifTestISPE(640, 480)
	for range 8 {
		nested = heifTestBox("ipco", nested)
	}

	// A top-level meta box declaring a 64-bit extended size after a 12-byte
	// ftyp box. MaxInt64 makes a naive offset+size bounds check overflow;
	// MaxUint64 becomes negative when converted to int.
	extendedSizeFile := func(extendedSize uint64) []byte {
		data := heifTestBox("ftyp", []byte("heic"))
		data = append(data, 0, 0, 0, 1)
		data = append(data, "meta"...)
		data = binary.BigEndian.AppendUint64(data, extendedSize)
		return append(data, make([]byte, 16)...)
	}

	tests := []struct {
		name       string
		data       []byte
		wantWidth  int
		wantHeight int
		wantOK     bool
	}{
		{
			name:       "minimal heic",
			data:       heifTestFile(heifTestBox("iprp", heifTestBox("ipco", heifTestISPE(4032, 3024)))),
			wantWidth:  4032,
			wantHeight: 3024,
			wantOK:     true,
		},
		{
			name: "container nesting beyond limit",
			data: heifTestFile(heifTestBox("iprp", nested)),
		},
		{
			name: "64-bit box size overflowing int addition",
			data: extendedSizeFile(math.MaxInt64),
		},
		{
			name: "64-bit box size negative as int",
			data: extendedSizeFile(math.MaxUint64),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var width, height int
			var ok bool
			assert.NotPanics(t, func() {
				width, height, ok = parseHEIFDimensions(tt.data)
			})
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantWidth, width)
			assert.Equal(t, tt.wantHeight, height)
		})
	}
}
