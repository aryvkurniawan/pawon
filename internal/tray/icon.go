package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"math"
)

// health: kesehatan stack — menentukan warna ikon tray.
type health int

const (
	healthDown health = iota // panel tidak merespons / semua service mati
	healthWarn               // sebagian service jalan
	healthOK                 // semua service jalan
)

// colors: warna ikon per status. Dipilih supaya terbaca di taskbar terang
// maupun gelap (Windows 11 punya dua tema taskbar).
var colors = map[health]color.RGBA{
	healthOK:   {0x2E, 0xC4, 0x6B, 0xFF}, // hijau
	healthWarn: {0xE8, 0xA3, 0x1E, 0xFF}, // kuning
	healthDown: {0xD9, 0x3A, 0x3A, 0xFF}, // merah
}

// iconSize: tray Windows menampilkan 16x16, tapi loader ikon butuh sumber
// yang lebih besar untuk scaling DPI — 32x32 adalah pilihan aman.
const iconSize = 32

// statusImage menggambar cakram warna status dengan tepi lebih gelap.
// Tepi itu penting: tanpa kontras, lingkaran hijau di taskbar hijau/terang
// nyaris tidak terlihat.
func statusImage(h health) *image.RGBA {
	base := colors[h]
	edge := color.RGBA{
		R: byte(int(base.R) * 55 / 100),
		G: byte(int(base.G) * 55 / 100),
		B: byte(int(base.B) * 55 / 100),
		A: 0xFF,
	}
	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	c := float64(iconSize-1) / 2
	rOut, rIn := c, c-2.5
	for y := range iconSize {
		for x := range iconSize {
			d := math.Hypot(float64(x)-c, float64(y)-c)
			if d > rOut {
				continue // di luar lingkaran → transparan
			}
			if d > rIn {
				img.Set(x, y, edge)
				continue
			}
			img.Set(x, y, base)
		}
	}
	return img
}

// icoBytes membungkus image jadi file .ico 32x32 32bpp (BGRA).
//
// Format ICO: header 6 byte, satu direnti 16 byte, lalu bitmap. Data piksel
// adalah DIB: BITMAPINFOHEADER + XOR mask (BGRA, bottom-up) + AND mask
// (1bpp, baris dipadkan ke 4 byte). Karena kita memakai alpha channel,
// AND mask diisi nol supaya seluruh piksel dianggap buram oleh loader.
func icoBytes(img image.Image) ([]byte, error) {
	var xor bytes.Buffer
	for y := iconSize - 1; y >= 0; y-- { // bottom-up
		for x := 0; x < iconSize; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			xor.Write([]byte{byte(b >> 8), byte(g >> 8), byte(r >> 8), byte(a >> 8)})
		}
	}
	// 32px @1bpp = 4 byte per baris → sudah kelipatan 4, tanpa padding.
	and := make([]byte, iconSize*iconSize/8)

	const hdr = 40
	dib := new(bytes.Buffer)
	if err := binary.Write(dib, binary.LittleEndian, struct {
		Size          uint32
		Width, Height int32
		Planes, Bits  uint16
		Compression   uint32
		SizeImage     uint32
		XRes, YRes    int32
		ClrUsed       uint32
		ClrImportant  uint32
	}{uint32(hdr), iconSize, iconSize * 2, 1, 32, 0,
		uint32(xor.Len() + len(and)), 0, 0, 0, 0}); err != nil {
		return nil, err
	}
	dib.Write(xor.Bytes())
	dib.Write(and)

	out := new(bytes.Buffer)
	if err := binary.Write(out, binary.LittleEndian, struct {
		Reserved, Type, Count uint16
	}{0, 1, 1}); err != nil {
		return nil, err
	}
	// width/height 32 dilaporkan sebagai 32 (nilai 0 berarti 256).
	if err := binary.Write(out, binary.LittleEndian, struct {
		W, H, ColorCount, Reserved uint8
		Planes, Bits               uint16
		BytesInRes, Offset         uint32
	}{iconSize, iconSize, 0, 0, 1, 32, uint32(dib.Len()), 6 + 16}); err != nil {
		return nil, err
	}
	out.Write(dib.Bytes())
	return out.Bytes(), nil
}

func iconBytes(h health) ([]byte, error) { return icoBytes(statusImage(h)) }
