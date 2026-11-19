package core

import (
	"errors"
	"strings"
)

// Small QR code encoder: alphanumeric mode, error correction level M,
// versions 1-4. Enough for "PIQLINK:" + a 12 character code.

const qrAlnum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"

type qrVersion struct {
	dataCW, ecPerBlock, blocks int
	align                      []int
}

var qrVersions = []qrVersion{
	{16, 10, 1, nil},          // v1-M
	{28, 16, 1, []int{6, 18}}, // v2-M
	{44, 26, 1, []int{6, 22}}, // v3-M
	{64, 18, 2, []int{6, 26}}, // v4-M
}

// QR is a generated symbol. Modules[y][x] true = dark.
type QR struct {
	Size    int
	Modules [][]bool
}

type bitBuf struct {
	b []byte
	n int
}

func (bb *bitBuf) put(v uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		if bb.n%8 == 0 {
			bb.b = append(bb.b, 0)
		}
		if v>>uint(i)&1 == 1 {
			bb.b[bb.n/8] |= 0x80 >> uint(bb.n%8)
		}
		bb.n++
	}
}

// GF(256) arithmetic, polynomial 0x11D
var gfExp [512]byte
var gfLog [256]byte

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

func rsEncode(data []byte, ecLen int) []byte {
	// generator polynomial (x - a^0)(x - a^1)...(x - a^(ecLen-1)), leading 1 omitted
	gen := make([]byte, ecLen)
	gen[ecLen-1] = 1
	root := byte(1)
	for i := 0; i < ecLen; i++ {
		for j := 0; j < ecLen; j++ {
			gen[j] = gfMul(gen[j], root)
			if j+1 < ecLen {
				gen[j] ^= gen[j+1]
			}
		}
		root = gfMul(root, 2)
	}
	res := make([]byte, ecLen)
	for _, d := range data {
		f := d ^ res[0]
		copy(res, res[1:])
		res[ecLen-1] = 0
		for i := range res {
			res[i] ^= gfMul(gen[i], f)
		}
	}
	return res
}

// EncodeQR builds a QR code for text (alphanumeric characters only).
func EncodeQR(text string) (*QR, error) {
	text = strings.ToUpper(text)
	for _, r := range text {
		if !strings.ContainsRune(qrAlnum, r) {
			return nil, errors.New("carattere non supportato nel QR")
		}
	}
	bitsNeeded := 4 + 9 + 11*(len(text)/2) + 6*(len(text)%2)
	vi := -1
	for i, v := range qrVersions {
		if bitsNeeded <= v.dataCW*8 {
			vi = i
			break
		}
	}
	if vi < 0 {
		return nil, errors.New("testo troppo lungo per il QR")
	}
	ver := qrVersions[vi]
	version := vi + 1

	var bb bitBuf
	bb.put(0b0010, 4)
	bb.put(uint32(len(text)), 9)
	for i := 0; i+1 < len(text); i += 2 {
		v := strings.IndexByte(qrAlnum, text[i])*45 + strings.IndexByte(qrAlnum, text[i+1])
		bb.put(uint32(v), 11)
	}
	if len(text)%2 == 1 {
		bb.put(uint32(strings.IndexByte(qrAlnum, text[len(text)-1])), 6)
	}
	capBits := ver.dataCW * 8
	term := capBits - bb.n
	if term > 4 {
		term = 4
	}
	bb.put(0, term)
	if bb.n%8 != 0 {
		bb.put(0, 8-bb.n%8)
	}
	for pad := byte(0xEC); len(bb.b) < ver.dataCW; pad ^= 0xEC ^ 0x11 {
		bb.b = append(bb.b, pad)
	}

	// split into blocks, add error correction, interleave
	blockLen := ver.dataCW / ver.blocks
	var dataBlocks, ecBlocks [][]byte
	for b := 0; b < ver.blocks; b++ {
		d := bb.b[b*blockLen : (b+1)*blockLen]
		dataBlocks = append(dataBlocks, d)
		ecBlocks = append(ecBlocks, rsEncode(d, ver.ecPerBlock))
	}
	var final []byte
	for i := 0; i < blockLen; i++ {
		for _, d := range dataBlocks {
			final = append(final, d[i])
		}
	}
	for i := 0; i < ver.ecPerBlock; i++ {
		for _, e := range ecBlocks {
			final = append(final, e[i])
		}
	}

	size := 17 + 4*version
	m := newMatrix(size)
	fn := newMatrix(size)
	set := func(x, y int, dark bool) { m[y][x] = dark; fn[y][x] = true }

	// finder patterns + separators
	for _, c := range [][2]int{{3, 3}, {size - 4, 3}, {3, size - 4}} {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := c[0]+dx, c[1]+dy
				if x < 0 || y < 0 || x >= size || y >= size {
					continue
				}
				d := max(abs(dx), abs(dy))
				set(x, y, d != 2 && d != 4)
			}
		}
	}
	// timing
	for i := 8; i < size-8; i++ {
		set(6, i, i%2 == 0)
		set(i, 6, i%2 == 0)
	}
	// alignment
	al := ver.align
	for i := range al {
		for j := range al {
			if (i == 0 && j == 0) || (i == 0 && j == len(al)-1) || (i == len(al)-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					set(al[i]+dx, al[j]+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	// reserve format areas (filled later) and dark module
	drawFormat(set, size, 0)

	// data placement (zigzag)
	bit := 0
	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				upward := (right+1)&2 == 0
				y := vert
				if upward {
					y = size - 1 - vert
				}
				if !fn[y][x] && bit < len(final)*8 {
					m[y][x] = final[bit>>3]>>(7-uint(bit&7))&1 == 1
					bit++
				}
			}
		}
	}

	// choose the mask with the lowest penalty
	best, bestPen := 0, -1
	for mask := 0; mask < 8; mask++ {
		t := copyMatrix(m)
		applyMask(t, fn, mask)
		drawFormat(func(x, y int, d bool) { t[y][x] = d }, size, mask)
		if p := penalty(t); bestPen < 0 || p < bestPen {
			best, bestPen = mask, p
		}
	}
	applyMask(m, fn, best)
	drawFormat(func(x, y int, d bool) { m[y][x] = d }, size, best)
	return &QR{Size: size, Modules: m}, nil
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func newMatrix(n int) [][]bool {
	m := make([][]bool, n)
	for i := range m {
		m[i] = make([]bool, n)
	}
	return m
}

func copyMatrix(m [][]bool) [][]bool {
	c := newMatrix(len(m))
	for i := range m {
		copy(c[i], m[i])
	}
	return c
}

func drawFormat(set func(x, y int, d bool), size, mask int) {
	data := 0<<3 | mask // ECC level M = 00
	rem := data
	for i := 0; i < 10; i++ {
		rem = rem<<1 ^ (rem>>9)*0x537
	}
	bits := (data<<10 | rem) ^ 0x5412
	b := func(i int) bool { return bits>>uint(i)&1 == 1 }
	for i := 0; i <= 5; i++ {
		set(8, i, b(i))
	}
	set(8, 7, b(6))
	set(8, 8, b(7))
	set(7, 8, b(8))
	for i := 9; i < 15; i++ {
		set(14-i, 8, b(i))
	}
	for i := 0; i < 8; i++ {
		set(size-1-i, 8, b(i))
	}
	for i := 8; i < 15; i++ {
		set(8, size-15+i, b(i))
	}
	set(8, size-8, true)
}

func applyMask(m, fn [][]bool, mask int) {
	for y := range m {
		for x := range m[y] {
			if fn[y][x] {
				continue
			}
			var inv bool
			switch mask {
			case 0:
				inv = (x+y)%2 == 0
			case 1:
				inv = y%2 == 0
			case 2:
				inv = x%3 == 0
			case 3:
				inv = (x+y)%3 == 0
			case 4:
				inv = (x/3+y/2)%2 == 0
			case 5:
				inv = x*y%2+x*y%3 == 0
			case 6:
				inv = (x*y%2+x*y%3)%2 == 0
			case 7:
				inv = ((x+y)%2+x*y%3)%2 == 0
			}
			if inv {
				m[y][x] = !m[y][x]
			}
		}
	}
}

func penalty(m [][]bool) int {
	n := len(m)
	p := 0
	get := func(x, y int, vertical bool) bool {
		if vertical {
			return m[x][y]
		}
		return m[y][x]
	}
	for _, vertical := range []bool{false, true} {
		for y := 0; y < n; y++ {
			run := 1
			for x := 1; x <= n; x++ {
				if x < n && get(x, y, vertical) == get(x-1, y, vertical) {
					run++
					continue
				}
				if run >= 5 {
					p += 3 + run - 5
				}
				run = 1
			}
			// finder-like patterns
			for x := 0; x+11 <= n; x++ {
				pat1, pat2 := true, true
				a := []bool{true, false, true, true, true, false, true, false, false, false, false}
				for k := 0; k < 11; k++ {
					v := get(x+k, y, vertical)
					if v != a[k] {
						pat1 = false
					}
					if v != a[10-k] {
						pat2 = false
					}
				}
				if pat1 {
					p += 40
				}
				if pat2 {
					p += 40
				}
			}
		}
	}
	dark := 0
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if m[y][x] {
				dark++
			}
			if x+1 < n && y+1 < n && m[y][x] == m[y][x+1] && m[y][x] == m[y+1][x] && m[y][x] == m[y+1][x+1] {
				p += 3
			}
		}
	}
	pct := dark * 100 / (n * n)
	p += abs(pct-50) / 5 * 10
	return p
}
