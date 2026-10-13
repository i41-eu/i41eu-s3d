package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/i41-eu/i41eu-assets/robot3d"
)

// glb reads back what build wrote: the document and the binary chunk.
func glb(t *testing.T, b []byte) (document, []byte) {
	t.Helper()
	if len(b) < 20 || binary.LittleEndian.Uint32(b) != 0x46546C67 || binary.LittleEndian.Uint32(b[4:]) != 2 {
		t.Fatal("not a glTF 2 binary")
	}
	if int(binary.LittleEndian.Uint32(b[8:])) != len(b) {
		t.Fatal("wrong length")
	}
	jl := int(binary.LittleEndian.Uint32(b[12:]))
	var doc document
	if err := json.Unmarshal(b[20:20+jl], &doc); err != nil {
		t.Fatal(err)
	}
	bin := b[20+jl+8:]
	if len(doc.Buffers) != 1 || doc.Buffers[0].ByteLength != len(bin) {
		t.Fatal("buffer length")
	}
	return doc, bin
}

// m4 is row major for column vectors, like robot3d's.
type m4 [16]float64

func ident() m4 { return m4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }

func (a m4) mul(b m4) m4 {
	var m m4
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			for k := 0; k < 4; k++ {
				m[r*4+c] += a[r*4+k] * b[k*4+c]
			}
		}
	}
	return m
}

func (a m4) point(p robot3d.V3) robot3d.V3 {
	return robot3d.V3{X: a[0]*p.X + a[1]*p.Y + a[2]*p.Z + a[3], Y: a[4]*p.X + a[5]*p.Y + a[6]*p.Z + a[7], Z: a[8]*p.X + a[9]*p.Y + a[10]*p.Z + a[11]}
}

func trans(v robot3d.V3) m4 { return m4{1, 0, 0, v.X, 0, 1, 0, v.Y, 0, 0, 1, v.Z, 0, 0, 0, 1} }

func rotX(a float64) m4 {
	s, c := math.Sincos(a)
	return m4{1, 0, 0, 0, 0, c, -s, 0, 0, s, c, 0, 0, 0, 0, 1}
}

func rotY(a float64) m4 {
	s, c := math.Sincos(a)
	return m4{c, 0, s, 0, 0, 1, 0, 0, -s, 0, c, 0, 0, 0, 0, 1}
}

func local(n node) m4 {
	m := ident()
	if n.Translation != nil {
		m = trans(robot3d.V3{X: n.Translation[0], Y: n.Translation[1], Z: n.Translation[2]})
	}
	if n.Scale != nil {
		m = m.mul(m4{n.Scale[0], 0, 0, 0, 0, n.Scale[1], 0, 0, 0, 0, n.Scale[2], 0, 0, 0, 0, 1})
	}
	return m
}

// world gives each node's matrix, a joint's pose (pose[name]) applied after its own transform,
// the robot node's metres back to millimetres (as a page with the robot at scale 1000 sees it).
func world(doc document, pose map[string]m4) map[string]m4 {
	out := map[string]m4{}
	var walk func(i int, parent m4)
	walk = func(i int, parent m4) {
		n := doc.Nodes[i]
		m := parent.mul(local(n))
		if p, ok := pose[n.Name]; ok {
			m = m.mul(p)
		}
		out[n.Name] = m
		for _, c := range n.Children {
			walk(c, m)
		}
	}
	walk(doc.Scenes[doc.Scene].Nodes[0], m4{1000, 0, 0, 0, 0, 1000, 0, 0, 0, 0, 1000, 0, 0, 0, 0, 1})
	return out
}

// triangles gives a mesh's vertices in index order: three per triangle, as robot3d lists them.
func triangles(t *testing.T, doc document, bin []byte, mesh int) []robot3d.V3 {
	t.Helper()
	pr := doc.Meshes[mesh].Primitives[0]
	data := func(a int) ([]byte, accessor) {
		acc := doc.Accessors[a]
		v := doc.BufferViews[acc.BufferView]
		return bin[v.ByteOffset : v.ByteOffset+v.ByteLength], acc
	}
	pos, pa := data(pr.Attributes["POSITION"])
	idx, ia := data(pr.Indices)
	if pa.ComponentType != glFloat || ia.ComponentType != glUnsignedInt {
		t.Fatal("component types")
	}
	f := func(o int) float64 { return float64(math.Float32frombits(binary.LittleEndian.Uint32(pos[o:]))) }
	out := make([]robot3d.V3, ia.Count)
	for i := range out {
		j := int(binary.LittleEndian.Uint32(idx[4*i:]))
		if j >= pa.Count {
			t.Fatalf("index %d out of %d", j, pa.Count)
		}
		out[i] = robot3d.V3{X: f(12 * j), Y: f(12*j + 4), Z: f(12*j + 8)}
	}
	return out
}

// normals gives a mesh's normals in index order, like triangles.
func normals(t *testing.T, doc document, bin []byte, mesh int) []robot3d.V3 {
	t.Helper()
	pr := doc.Meshes[mesh].Primitives[0]
	na, ia := doc.Accessors[pr.Attributes["NORMAL"]], doc.Accessors[pr.Indices]
	nv, iv := doc.BufferViews[na.BufferView], doc.BufferViews[ia.BufferView]
	nb, ib := bin[nv.ByteOffset:nv.ByteOffset+nv.ByteLength], bin[iv.ByteOffset:iv.ByteOffset+iv.ByteLength]
	f := func(o int) float64 { return float64(math.Float32frombits(binary.LittleEndian.Uint32(nb[o:]))) }
	out := make([]robot3d.V3, ia.Count)
	for i := range out {
		j := int(binary.LittleEndian.Uint32(ib[4*i:]))
		out[i] = robot3d.V3{X: f(12 * j), Y: f(12*j + 4), Z: f(12*j + 8)}
	}
	return out
}

func near(a, b robot3d.V3, tol float64) bool { return a.Sub(b).Len() <= tol }

func export(t *testing.T) (document, []byte) {
	t.Helper()
	centre, w, h := robot3d.Screen()
	b, err := build(robot3d.Parts(), robot3d.PitchPivot(), centre, w, h, nil, robot3d.Surface)
	if err != nil {
		t.Fatal(err)
	}
	return glb(t, b)
}

// Every part robot3d has is a node with its mesh under its joint, and every triangle is where
// robot3d has it at rest.
func TestPartsPlaced(t *testing.T) {
	doc, bin := export(t)
	byName := map[string]node{}
	parentOf := map[string]string{}
	for _, n := range doc.Nodes {
		byName[n.Name] = n
		for _, c := range n.Children {
			parentOf[doc.Nodes[c].Name] = n.Name
		}
	}
	if parentOf["base"] != "robot" || parentOf["yaw"] != "base" || parentOf["head"] != "yaw" {
		t.Fatalf("joints: %v", parentOf)
	}
	at := world(doc, nil)
	parts := robot3d.Parts()
	if len(parts) != 15 {
		t.Errorf("robot3d has %d parts, want 15", len(parts))
	}
	for _, p := range parts {
		n, ok := byName[p.Name]
		if !ok || n.Mesh == nil {
			t.Errorf("%s: no node with a mesh", p.Name)
			continue
		}
		if parentOf[p.Name] != p.Joint {
			t.Errorf("%s: under %q, robot3d's joint is %q", p.Name, parentOf[p.Name], p.Joint)
		}
		got := triangles(t, doc, bin, *n.Mesh)
		if len(got) != len(p.Positions) {
			t.Errorf("%s: %d vertices, robot3d %d", p.Name, len(got), len(p.Positions))
			continue
		}
		bad := 0 // a triangle's last two vertices may be swapped (orient)
		for i := 0; i < len(got); i += 3 {
			a, b, c := at[p.Name].point(got[i]), at[p.Name].point(got[i+1]), at[p.Name].point(got[i+2])
			q := p.Positions[i : i+3]
			if !near(a, q[0], 1e-4) || !(near(b, q[1], 1e-4) && near(c, q[2], 1e-4) || near(b, q[2], 1e-4) && near(c, q[1], 1e-4)) {
				bad++
			}
		}
		if bad > 0 {
			t.Errorf("%s: %d of %d triangles not where robot3d has them", p.Name, bad, len(got)/3)
		}
	}
}

// A few points by hand: the plate on the ground, the head's top at 70.5 mm, the LED bars on
// the sides, the screen's corners at robot3d.Screen().
func TestPoints(t *testing.T) {
	doc, bin := export(t)
	at := world(doc, nil)
	bounds := func(name string) (lo, hi robot3d.V3) {
		for _, n := range doc.Nodes {
			if n.Name == name {
				vs := triangles(t, doc, bin, *n.Mesh)
				lo, hi = at[name].point(vs[0]), at[name].point(vs[0])
				for _, v := range vs {
					w := at[name].point(v)
					lo = robot3d.V3{X: min(lo.X, w.X), Y: min(lo.Y, w.Y), Z: min(lo.Z, w.Z)}
					hi = robot3d.V3{X: max(hi.X, w.X), Y: max(hi.Y, w.Y), Z: max(hi.Z, w.Z)}
				}
			}
		}
		return
	}
	if lo, _ := bounds("plate"); math.Abs(lo.Y) > 1e-3 {
		t.Errorf("plate's bottom at y %.3f, want 0", lo.Y)
	}
	if _, hi := bounds("core"); math.Abs(hi.Y-70.5) > 1e-3 || math.Abs(hi.Z-33.6) > 1e-3 {
		t.Errorf("core's top front at %+v, want y 70.5, z 33.6", hi)
	}
	// The LED bars as the firmware names its strips: left (LEDs 0-5) on the left as you face the
	// screen (-X, the robot's own right side), right (6-11) on +X.
	if _, hi := bounds("led-bar-left"); hi.X > -20 {
		t.Errorf("led-bar-left at x %.2f: not on the left as you face the screen (-X)", hi.X)
	}
	if lo, _ := bounds("led-bar-right"); lo.X < 20 {
		t.Errorf("led-bar-right at x %.2f: not on the right as you face the screen (+X)", lo.X)
	}
	c, w, h := robot3d.Screen()
	lo, hi := bounds("screen")
	if !near(lo, robot3d.V3{X: c.X - w/2, Y: c.Y - h/2, Z: c.Z + screenLift}, 1e-3) || !near(hi, robot3d.V3{X: c.X + w/2, Y: c.Y + h/2, Z: c.Z + screenLift}, 1e-3) {
		t.Errorf("screen from %+v to %+v, robot3d's centre %+v, %gx%g", lo, hi, c, w, h)
	}
}

// Turned: the yaw node about Y and the head node about X by -pitch move a head point as
// robot3d does (rotY(yaw) · T(pivot) · rotX(-pitch) · T(-pivot)); the plate stays.
func TestPose(t *testing.T) {
	doc, _ := export(t)
	pivot := robot3d.PitchPivot()
	for _, pose := range [][2]float64{{30, 0}, {0, 20}, {-45, 60}} {
		yaw, pitch := pose[0]*math.Pi/180, pose[1]*math.Pi/180
		at := world(doc, map[string]m4{"yaw": rotY(yaw), "head": rotX(-pitch)})
		want := rotY(yaw).mul(trans(pivot)).mul(rotX(-pitch)).mul(trans(pivot.Mul(-1)))
		for _, p := range []robot3d.V3{{X: 0, Y: 44.5, Z: 33.6}, {X: 26.1, Y: 69.4, Z: -1.65}, {X: -27, Y: 16.5, Z: 18.1}} {
			for _, name := range []string{"core", "body", "led-bar-left", "screen"} {
				if got := at[name].point(p); !near(got, want.point(p), 1e-6) {
					t.Errorf("yaw %v pitch %v %s: %+v → %+v, robot3d %+v", pose[0], pose[1], name, p, got, want.point(p))
				}
			}
			if got := at["servo"].point(p); !near(got, rotY(yaw).point(p), 1e-6) {
				t.Errorf("servo: %+v, want %+v", got, rotY(yaw).point(p))
			}
			if got := at["plate"].point(p); !near(got, p, 1e-6) {
				t.Errorf("plate moved: %+v", got)
			}
		}
		// +pitch lifts the front: the screen's centre goes up.
		if pose[1] > 0 && pose[0] == 0 {
			if c := at["screen"].point(robot3d.V3{X: 0, Y: 44.5, Z: 33.6}); c.Y <= 44.5 {
				t.Errorf("pitch %v: the screen's centre at y %.2f, not lifted", pose[1], c.Y)
			}
		}
	}
}

// The committed web/robot.glb is what s3dgen writes now (with its default screen picture, from
// the assets cloned beside this repo; skipped without them).
func TestCommittedUpToDate(t *testing.T) {
	png, err := os.ReadFile(filepath.Join("../..", defaultScreen)) // tests run in cmd/s3dgen
	if err != nil {
		t.Skip("no ../i41eu-assets (setup.sh)")
	}
	decals, err := loadDecals("../../data/decals.json")
	if err != nil {
		t.Fatal(err)
	}
	centre, w, h := robot3d.Screen()
	want, err := build(robot3d.Parts(), robot3d.PitchPivot(), centre, w, h, png, withDecals(robot3d.Surface, decals))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../web/robot.glb")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("web/robot.glb is not what s3dgen writes: go run ./cmd/s3dgen")
	}
}

// The textured parts show robot3d's details where robot3d draws them: at each point, the triangle
// of the exported mesh that holds it, its UV, the texel there in the part's texture, which is
// robot3d.Surface's colour for that point.
func TestTextures(t *testing.T) {
	doc, bin := export(t)
	data := func(a int) ([]byte, accessor) {
		acc := doc.Accessors[a]
		v := doc.BufferViews[acc.BufferView]
		return bin[v.ByteOffset : v.ByteOffset+v.ByteLength], acc
	}
	f32 := func(b []byte, o int) float64 { return float64(math.Float32frombits(binary.LittleEndian.Uint32(b[o:]))) }
	core, _, _ := robot3d.Screen()
	for _, c := range []struct {
		what, part string
		p, n       robot3d.V3
	}{
		{"the glass front", "core", core, robot3d.V3{Z: 1}},
		{"the red ring", "core", robot3d.V3{X: 1.55, Y: 43.5 - 20.3, Z: 33.6}, robot3d.V3{Z: 1}},
		{"the power button", "core", robot3d.V3{X: -27, Y: 56.4, Z: 25.25}, robot3d.V3{X: -1}},
		{"the power button's black ring", "core", robot3d.V3{X: -27, Y: 56.4, Z: 29.75}, robot3d.V3{X: -1}},
		{"the power sign above it", "core", robot3d.V3{X: -27, Y: 63.3, Z: 26.25}, robot3d.V3{X: -1}},
		{"the USB-C port", "core", robot3d.V3{X: -27, Y: 43.5, Z: 25.45}, robot3d.V3{X: -1}},
		{"the board's ribbon cable, through the open back", "core", robot3d.V3{X: -7, Y: 53.5, Z: 17.4}, robot3d.V3{Z: -1}},
		{"the board's white connector", "core", robot3d.V3{X: 11.5, Y: 61.2, Z: 17.4}, robot3d.V3{Z: -1}},
		{"the left label's dark end", "body", robot3d.V3{X: 27.03, Y: 60, Z: 13.5}, robot3d.V3{X: 1}},
		{"the body's red LED", "servo", robot3d.V3{X: 0.25, Y: 14.3, Z: -23.15}, robot3d.V3{Z: -1}},
		{"the pitch servo's light grey body", "pitch-servo", robot3d.V3{X: -2.4, Y: 47.3, Z: -22}, robot3d.V3{Z: -1}},
		{"the pitch servo's dark horn", "pitch-servo", robot3d.V3{X: 16.5, Y: 44, Z: -17}, robot3d.V3{Z: -1}},
		{"the back cover", "servo-cover", robot3d.V3{X: 0, Y: 25, Z: -22.39}, robot3d.V3{Z: -1}},
		{"the stand's USB-C", "plate", robot3d.V3{X: 3.1, Y: 7.55, Z: -23.3}, robot3d.V3{Z: -1}},
	} {
		want, _ := robot3d.Surface(c.part, c.p, c.n)
		var pr primitive
		for _, n := range doc.Nodes {
			if n.Name == c.part {
				pr = doc.Meshes[*n.Mesh].Primitives[0]
			}
		}
		m := doc.Materials[pr.Material]
		if !m.DoubleSided || m.PBRMetallicRoughness.BaseColorTexture == nil || m.PBRMetallicRoughness.MetallicRoughnessTexture == nil {
			t.Fatalf("%s: material %+v", c.part, m)
		}
		tex := doc.Textures[m.PBRMetallicRoughness.BaseColorTexture.Index]
		iv := doc.BufferViews[doc.Images[tex.Source].BufferView]
		img, err := png.Decode(bytes.NewReader(bin[iv.ByteOffset : iv.ByteOffset+iv.ByteLength]))
		if err != nil {
			t.Fatal(err)
		}
		pos, _ := data(pr.Attributes["POSITION"])
		nrm, _ := data(pr.Attributes["NORMAL"])
		uvs, _ := data(pr.Attributes["TEXCOORD_0"])
		idx, ia := data(pr.Indices)
		// The axes of the face's plane.
		u, v := 0, 1
		switch {
		case c.n.X != 0:
			u, v = 1, 2
		case c.n.Y != 0:
			u, v = 0, 2
		}
		found := false
		for i := 0; i < ia.Count && !found; i += 3 {
			var tri [3]robot3d.V3
			var tuv [3][2]float64
			facing := 0.0
			for k := 0; k < 3; k++ {
				j := int(binary.LittleEndian.Uint32(idx[4*(i+k):]))
				tri[k] = robot3d.V3{X: f32(pos, 12*j), Y: f32(pos, 12*j+4), Z: f32(pos, 12*j+8)}
				facing += robot3d.V3{X: f32(nrm, 12*j), Y: f32(nrm, 12*j+4), Z: f32(nrm, 12*j+8)}.Dot(c.n)
				tuv[k] = [2]float64{f32(uvs, 8*j), f32(uvs, 8*j+4)}
			}
			if facing < 2.7 || math.Abs(tri[0].Sub(c.p).Dot(c.n)) > 0.3 {
				continue // not on this face
			}
			// Barycentric coordinates in the face's plane.
			ax := func(p robot3d.V3, i int) float64 { return [3]float64{p.X, p.Y, p.Z}[i] }
			x0, y0 := ax(tri[0], u), ax(tri[0], v)
			x1, y1 := ax(tri[1], u)-x0, ax(tri[1], v)-y0
			x2, y2 := ax(tri[2], u)-x0, ax(tri[2], v)-y0
			px, py := ax(c.p, u)-x0, ax(c.p, v)-y0
			d := x1*y2 - x2*y1
			if math.Abs(d) < 1e-12 {
				continue
			}
			b1, b2 := (px*y2-x2*py)/d, (x1*py-px*y1)/d
			if b1 < -1e-6 || b2 < -1e-6 || b1+b2 > 1+1e-6 {
				continue
			}
			s := tuv[0][0] + b1*(tuv[1][0]-tuv[0][0]) + b2*(tuv[2][0]-tuv[0][0])
			r := tuv[0][1] + b1*(tuv[1][1]-tuv[0][1]) + b2*(tuv[2][1]-tuv[0][1])
			b := img.Bounds()
			got := color.RGBAModel.Convert(img.At(int(s*float64(b.Dx())), int(r*float64(b.Dy())))).(color.RGBA)
			if got != want {
				t.Errorf("%s: texel %v, robot3d %v", c.what, got, want)
			}
			found = true
		}
		if !found {
			t.Errorf("%s: no triangle of %s at %+v", c.what, c.part, c.p)
		}
	}
}

// openEdgesMax is each part's most open edges (an edge of one triangle only: a hole in its
// surface); it must not grow. All of robot3d's parts are closed now.
var openEdgesMax = map[string]int{
	"plate": 0, "servo": 0, "servo-cover": 0, "pitch-servo": 0, "battery": 0, "body": 0, "core": 0,
	"led-bar-left": 0, "led-bar-right": 0, "top-board": 0, "base-cover": 0,
	"led-board-left": 0, "led-board-right": 0, "led-chips-left": 0, "led-chips-right": 0,
}

// The exported parts have no holes: per part, its open edges (counted on the vertices at 0.1 µm)
// no more than openEdgesMax, and no triangle wound against its normals (seen from its back, a
// double-sided material lights it inward: dark, like a hole).
func TestMeshesClosed(t *testing.T) {
	doc, bin := export(t)
	type vk [3]int64
	q := func(v robot3d.V3) vk {
		return vk{int64(math.Round(v.X * 1e4)), int64(math.Round(v.Y * 1e4)), int64(math.Round(v.Z * 1e4))}
	}
	seen := 0
	for _, n := range doc.Nodes {
		if n.Mesh == nil || n.Name == "screen" {
			continue
		}
		want, ok := openEdgesMax[n.Name]
		if !ok {
			t.Errorf("%s: not in openEdgesMax", n.Name)
			continue
		}
		seen++
		pos, nrm := triangles(t, doc, bin, *n.Mesh), normals(t, doc, bin, *n.Mesh)
		edges := map[[2]vk]int{}
		inward := 0
		for i := 0; i < len(pos); i += 3 {
			face := pos[i+1].Sub(pos[i]).Cross(pos[i+2].Sub(pos[i]))
			if face.Dot(nrm[i].Add(nrm[i+1]).Add(nrm[i+2])) < 0 {
				inward++
			}
			for k := 0; k < 3; k++ {
				a, b := q(pos[i+k]), q(pos[i+(k+1)%3])
				if a == b {
					continue
				}
				if fmt.Sprint(a) > fmt.Sprint(b) {
					a, b = b, a
				}
				edges[[2]vk{a, b}]++
			}
		}
		open := 0
		for _, c := range edges {
			if c == 1 {
				open++
			}
		}
		if open > want {
			t.Errorf("%s: %d open edges, at most %d", n.Name, open, want)
		}
		if inward > 0 {
			t.Errorf("%s: %d of %d triangles wound against their normals", n.Name, inward, len(pos)/3)
		}
	}
	if seen != len(openEdgesMax) {
		t.Errorf("%d parts checked, openEdgesMax has %d", seen, len(openEdgesMax))
	}
}

// The decals: each on a part robot3d has, its picture there, its corners a rectangle on the face
// it is painted on.
func TestDecals(t *testing.T) {
	decals, err := loadDecals("../../data/decals.json")
	if err != nil {
		t.Fatal(err)
	}
	model := map[string]bool{}
	for _, p := range robot3d.Parts() {
		model[p.Name] = true
	}
	for _, dc := range decals {
		if !model[dc.Part] || dc.About == "" {
			t.Errorf("%s: part %q, about %q", dc.Image, dc.Part, dc.About)
		}
		tl := v3(dc.TopLeft)
		u, v := v3(dc.TopRight).Sub(tl), v3(dc.BottomLeft).Sub(tl)
		f := v3(dc.Face)
		if math.Abs(u.Dot(v)) > 1e-6 || math.Abs(u.Dot(f)) > 1e-6 || math.Abs(v.Dot(f)) > 1e-6 || u.Len() == 0 || v.Len() == 0 {
			t.Errorf("%s: corners not a rectangle on its face", dc.Image)
		}
	}
}

// A marker sits where the ray from outside along its place's out meets the robot within
// placeDepth of the place (web/app.js onSurface): on robot.glb at rest, every marker drawn on the
// surface lands there, and the places on the CoreS3's bottom land on the CoreS3, not on the
// stand under it (the first surface from outside, as the page took before).
func TestMarkersLand(t *testing.T) {
	const placeDepth, far = 4.0, 200.0 // mm, as in web/app.js
	b, err := os.ReadFile("../../data/components.json")
	if err != nil {
		t.Fatal(err)
	}
	type place struct {
		Part   string
		Offset [3]float64
		Out    []float64
		Area   string
		Shape  *struct{ Free bool }
	}
	var data struct {
		Components []struct {
			ID      string
			Where   *place
			Windows []struct {
				ID    string
				Where place
			}
		}
	}
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	doc, bin := export(t)
	at := world(doc, nil)
	type face struct {
		a, b, c robot3d.V3
		part    string
	}
	var faces []face
	for _, n := range doc.Nodes {
		if n.Mesh == nil {
			continue
		}
		vs := triangles(t, doc, bin, *n.Mesh)
		for i := 0; i < len(vs); i += 3 {
			faces = append(faces, face{at[n.Name].point(vs[i]), at[n.Name].point(vs[i+1]), at[n.Name].point(vs[i+2]), n.Name})
		}
	}
	// hits gives the distances along the ray at which it crosses each face, nearest first.
	type crossing struct {
		d    float64
		part string
	}
	hits := func(o, dir robot3d.V3) []crossing {
		var out []crossing
		for _, f := range faces {
			e1, e2 := f.b.Sub(f.a), f.c.Sub(f.a)
			p := dir.Cross(e2)
			det := e1.Dot(p)
			if math.Abs(det) < 1e-12 {
				continue
			}
			s := o.Sub(f.a)
			u := s.Dot(p) / det
			q := s.Cross(e1)
			v := dir.Dot(q) / det
			if u < 0 || v < 0 || u+v > 1 {
				continue
			}
			if d := e2.Dot(q) / det; d > 0 {
				out = append(out, crossing{d, f.part})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].d < out[j].d })
		return out
	}
	landed := map[string]string{}
	check := func(id string, w place) {
		if w.Area == "screen" || w.Shape != nil && w.Shape.Free {
			return
		}
		p := robot3d.V3{X: w.Offset[0], Y: w.Offset[1], Z: w.Offset[2]}
		out := robot3d.V3{X: p.X, Z: p.Z} // as the page: out from the yaw axis by default
		if len(w.Out) == 3 {
			out = robot3d.V3{X: w.Out[0], Y: w.Out[1], Z: w.Out[2]}
		}
		out = out.Mul(1 / out.Len())
		all := hits(p.Add(out.Mul(far)), out.Mul(-1))
		var first, then crossing
		for _, h := range all {
			if h.d <= far && first.part == "" {
				first = h // the page's old rule: the first surface from outside
			}
			if h.d >= far-placeDepth && h.d <= far+placeDepth && then.part == "" {
				then = h
			}
		}
		if then.part == "" {
			t.Errorf("%s: no surface within %g mm of its place %v along %v", id, placeDepth, w.Offset, out)
			return
		}
		landed[id] = then.part
		if first.part != "" && (first.part != then.part || math.Abs(first.d-then.d) > 1e-6) {
			t.Logf("%s: lands on %s %.1f mm from its place (the first surface from outside: %s, %.1f mm out)", id, then.part, far-then.d, first.part, far-first.d)
		}
	}
	for _, c := range data.Components {
		if c.Where != nil {
			check(c.ID, *c.Where)
		}
		for _, w := range c.Windows {
			check(c.ID+"/"+w.ID, w.Where)
		}
	}
	for _, id := range []string{"reset-button", "microsd-slot", "axp2101/led"} {
		if landed[id] != "core" {
			t.Errorf("%s: lands on %q, not on the CoreS3's bottom", id, landed[id])
		}
	}
}
