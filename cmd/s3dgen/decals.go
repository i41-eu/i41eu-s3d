package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"github.com/i41-eu/i41eu-assets/robot3d"
)

// decal is a picture painted on a part (data/decals.json): on its side facing Face, its corners
// at rest (mm, robot3d's space).
type decal struct {
	Part       string     `json:"part"`
	Image      string     `json:"image"`
	About      string     `json:"about"`
	Face       [3]float64 `json:"face"`
	TopLeft    [3]float64 `json:"topLeft"`
	TopRight   [3]float64 `json:"topRight"`
	BottomLeft [3]float64 `json:"bottomLeft"`
	img        image.Image
}

// loadDecals reads the decals and their pictures (in data/decals/ beside the file).
func loadDecals(path string) ([]decal, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		About  string  `json:"about"`
		Decals []decal `json:"decals"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range doc.Decals {
		dc := &doc.Decals[i]
		f, err := os.ReadFile(filepath.Join(filepath.Dir(path), "decals", dc.Image))
		if err != nil {
			return nil, err
		}
		if dc.img, err = png.Decode(bytes.NewReader(f)); err != nil {
			return nil, fmt.Errorf("%s: %w", dc.Image, err)
		}
	}
	return doc.Decals, nil
}

func v3(a [3]float64) robot3d.V3 { return robot3d.V3{X: a[0], Y: a[1], Z: a[2]} }

// at gives the decal's colour at a point of its part (p, n at rest), if the point is on it.
func (dc decal) at(p, n robot3d.V3) (color.RGBA, bool) {
	if n.Dot(v3(dc.Face)) < 0.9 {
		return color.RGBA{}, false
	}
	tl := v3(dc.TopLeft)
	u, v := v3(dc.TopRight).Sub(tl), v3(dc.BottomLeft).Sub(tl)
	d := p.Sub(tl)
	s, t := d.Dot(u)/u.Dot(u), d.Dot(v)/v.Dot(v)
	if s < 0 || s > 1 || t < 0 || t > 1 {
		return color.RGBA{}, false
	}
	b := dc.img.Bounds()
	x := b.Min.X + int(math.Min(float64(b.Dx()-1), s*float64(b.Dx())))
	y := b.Min.Y + int(math.Min(float64(b.Dy()-1), t*float64(b.Dy())))
	r, g, bl, _ := dc.img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), 255}, true
}

// withDecals is surface with the decals painted over it.
func withDecals(surface surfaceFunc, decals []decal) surfaceFunc {
	return func(part string, p, n robot3d.V3) (color.RGBA, bool) {
		for _, dc := range decals {
			if dc.Part == part {
				if c, ok := dc.at(p, n); ok {
					return c, false
				}
			}
		}
		return surface(part, p, n)
	}
}
