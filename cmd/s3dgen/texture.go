package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/i41-eu/i41eu-assets/robot3d"
)

// surfaceFunc is how a part looks at a point at rest, before the lights (robot3d.Surface).
type surfaceFunc func(part string, p, n robot3d.V3) (color.RGBA, bool)

// textured are the parts whose details robot3d draws (the glass front, the ring and the dots, the
// vents, the ports, the button, the labels, the board through the open back): baked into textures.
var textured = map[string]bool{"core": true, "body": true, "top-board": true, "base-cover": true}

const (
	texelsPerMM = 8 // a vent hole (1 mm) is 8 texels across
	cellPad     = 4 // texels round each cell, its edge repeated (no bleeding between cells)
	atlasWidth  = 1024
	onFace      = 2.0 // mm: a triangle further in than this from its face is inside (the open back)
	roughShell  = 0.6
	roughGlass  = 0.15
)

// cell is one face of a part's bounding box in the atlas: the face whose normal is sign along
// axis, its 2D axes u and v, its place in the atlas (texels, without the padding).
type cell struct {
	axis, u, v int
	sign       float64
	x, y, w, h int
}

// atlas is a part's texture layout: six faces and a plain texel for what is inside.
type atlas struct {
	lo, hi robot3d.V3
	cells  [6]cell
	plain  image.Point
	w, h   int
}

func axis(v robot3d.V3, i int) float64 { return [3]float64{v.X, v.Y, v.Z}[i] }

func setAxis(v *robot3d.V3, i int, x float64) {
	switch i {
	case 0:
		v.X = x
	case 1:
		v.Y = x
	default:
		v.Z = x
	}
}

func bounds(ps []robot3d.V3) (lo, hi robot3d.V3) {
	lo, hi = ps[0], ps[0]
	for _, p := range ps {
		lo = robot3d.V3{X: math.Min(lo.X, p.X), Y: math.Min(lo.Y, p.Y), Z: math.Min(lo.Z, p.Z)}
		hi = robot3d.V3{X: math.Max(hi.X, p.X), Y: math.Max(hi.Y, p.Y), Z: math.Max(hi.Z, p.Z)}
	}
	return
}

// layout places a part's six faces in rows, then the plain texel's cell.
func layout(lo, hi robot3d.V3) *atlas {
	a := &atlas{lo: lo, hi: hi}
	x, y, rowH := 0, 0, 0
	put := func(w, h int) (int, int) {
		if x+w+2*cellPad > atlasWidth {
			x, y, rowH = 0, y+rowH, 0
		}
		px, py := x+cellPad, y+cellPad
		x += w + 2*cellPad
		rowH = max(rowH, h+2*cellPad)
		return px, py
	}
	for k := range a.cells {
		ax, sign := k/2, []float64{1, -1}[k%2]
		u, v := (ax+1)%3, (ax+2)%3
		w := int(math.Ceil((axis(hi, u) - axis(lo, u)) * texelsPerMM))
		h := int(math.Ceil((axis(hi, v) - axis(lo, v)) * texelsPerMM))
		px, py := put(w, h)
		a.cells[k] = cell{axis: ax, u: u, v: v, sign: sign, x: px, y: py, w: w, h: h}
	}
	px, py := put(2, 2)
	a.plain = image.Pt(px, py)
	a.w, a.h = atlasWidth, y+rowH
	return a
}

// face gives a triangle's cell, or -1 when it is inside the part (away from every face). Its
// direction is its normals' (robot3d's triangles are not all wound the same way).
func (a *atlas) face(t, n [3]robot3d.V3) int {
	fn := n[0].Add(n[1]).Add(n[2])
	ax := 0
	for i := 1; i < 3; i++ {
		if math.Abs(axis(fn, i)) > math.Abs(axis(fn, ax)) {
			ax = i
		}
	}
	c := t[0].Add(t[1]).Add(t[2]).Mul(1.0 / 3)
	if axis(fn, ax) >= 0 {
		if axis(a.hi, ax)-axis(c, ax) > onFace {
			return -1
		}
		return 2 * ax
	}
	if axis(c, ax)-axis(a.lo, ax) > onFace {
		return -1
	}
	return 2*ax + 1
}

// uv is a point's texture coordinate on a cell (or the plain texel for -1).
func (a *atlas) uv(k int, p robot3d.V3) [2]float32 {
	if k < 0 {
		return [2]float32{float32(float64(a.plain.X)+1) / float32(a.w), float32(float64(a.plain.Y)+1) / float32(a.h)}
	}
	c := a.cells[k]
	s := float64(c.x) + (axis(p, c.u)-axis(a.lo, c.u))*texelsPerMM
	t := float64(c.y) + (axis(p, c.v)-axis(a.lo, c.v))*texelsPerMM
	return [2]float32{float32(s / float64(a.w)), float32(t / float64(a.h))}
}

// point is where a texel of a cell is on its face (padding: the nearest edge), and the face's normal.
func (a *atlas) point(k, px, py int) (p, n robot3d.V3) {
	c := a.cells[k]
	s := axis(a.lo, c.u) + (float64(px-c.x)+0.5)/texelsPerMM
	t := axis(a.lo, c.v) + (float64(py-c.y)+0.5)/texelsPerMM
	setAxis(&p, c.u, math.Max(axis(a.lo, c.u), math.Min(axis(a.hi, c.u), s)))
	setAxis(&p, c.v, math.Max(axis(a.lo, c.v), math.Min(axis(a.hi, c.v), t)))
	if c.sign > 0 {
		setAxis(&p, c.axis, axis(a.hi, c.axis))
	} else {
		setAxis(&p, c.axis, axis(a.lo, c.axis))
	}
	setAxis(&n, c.axis, c.sign)
	return
}

// paint draws the atlas: the colour (sRGB) and the metallic-roughness texture (G roughness,
// B metallic: 0) of every texel, from surface; the plain cell in the part's own colour.
func (a *atlas) paint(part robot3d.Part, surface surfaceFunc) (base, rough []byte) {
	col := image.NewRGBA(image.Rect(0, 0, a.w, a.h))
	mr := image.NewRGBA(image.Rect(0, 0, a.w, a.h))
	shell := color.RGBA{0, uint8(math.Round(roughShell * 255)), 0, 255}
	glass := color.RGBA{0, uint8(math.Round(roughGlass * 255)), 0, 255}
	for i := range col.Pix {
		col.Pix[i] = 0xff
	}
	for k, c := range a.cells {
		for py := c.y - cellPad; py < c.y+c.h+cellPad; py++ {
			for px := c.x - cellPad; px < c.x+c.w+cellPad; px++ {
				p, n := a.point(k, px, py)
				rgb, isGlass := surface(part.Name, p, n)
				col.SetRGBA(px, py, rgb)
				if isGlass {
					mr.SetRGBA(px, py, glass)
				} else {
					mr.SetRGBA(px, py, shell)
				}
			}
		}
	}
	for py := a.plain.Y - cellPad; py < a.plain.Y+2+cellPad; py++ {
		for px := a.plain.X - cellPad; px < a.plain.X+2+cellPad; px++ {
			col.SetRGBA(px, py, part.Color)
			mr.SetRGBA(px, py, shell)
		}
	}
	return encode(col), encode(mr)
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	e := png.Encoder{CompressionLevel: png.BestCompression}
	e.Encode(&buf, img)
	return buf.Bytes()
}
