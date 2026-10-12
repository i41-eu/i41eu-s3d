package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"

	"github.com/i41-eu/i41eu-assets/robot3d"
)

// The glTF 2.0 document, only what is written here.
type document struct {
	Asset          asset          `json:"asset"`
	ExtensionsUsed []string       `json:"extensionsUsed,omitempty"`
	Scene          int            `json:"scene"`
	Scenes         []scene        `json:"scenes"`
	Nodes          []node         `json:"nodes"`
	Meshes         []mesh         `json:"meshes"`
	Materials      []material     `json:"materials"`
	Textures       []texture      `json:"textures,omitempty"`
	Images         []gltfImage    `json:"images,omitempty"`
	Samplers       []sampler      `json:"samplers,omitempty"`
	Accessors      []accessor     `json:"accessors"`
	BufferViews    []bufferView   `json:"bufferViews"`
	Buffers        []buffer       `json:"buffers"`
	Extras         map[string]any `json:"extras,omitempty"`
}

type asset struct {
	Version   string `json:"version"`
	Generator string `json:"generator"`
	Copyright string `json:"copyright,omitempty"`
}

type scene struct {
	Name  string `json:"name"`
	Nodes []int  `json:"nodes"`
}

type node struct {
	Name        string         `json:"name"`
	Mesh        *int           `json:"mesh,omitempty"`
	Children    []int          `json:"children,omitempty"`
	Translation []float64      `json:"translation,omitempty"`
	Scale       []float64      `json:"scale,omitempty"`
	Extras      map[string]any `json:"extras,omitempty"`
}

type mesh struct {
	Name       string      `json:"name"`
	Primitives []primitive `json:"primitives"`
}

type primitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    int            `json:"indices"`
	Material   int            `json:"material"`
}

type material struct {
	Name                 string         `json:"name"`
	PBRMetallicRoughness pbr            `json:"pbrMetallicRoughness"`
	DoubleSided          bool           `json:"doubleSided,omitempty"`
	Extensions           map[string]any `json:"extensions,omitempty"`
}

type pbr struct {
	BaseColorFactor          []float64    `json:"baseColorFactor"`
	BaseColorTexture         *textureInfo `json:"baseColorTexture,omitempty"`
	MetallicRoughnessTexture *textureInfo `json:"metallicRoughnessTexture,omitempty"`
	MetallicFactor           float64      `json:"metallicFactor"`
	RoughnessFactor          float64      `json:"roughnessFactor"`
}

type textureInfo struct {
	Index int `json:"index"`
}

type texture struct {
	Sampler int `json:"sampler"`
	Source  int `json:"source"`
}

type gltfImage struct {
	BufferView int    `json:"bufferView"`
	MimeType   string `json:"mimeType"`
}

type sampler struct {
	MagFilter int `json:"magFilter"`
	MinFilter int `json:"minFilter"`
	WrapS     int `json:"wrapS"`
	WrapT     int `json:"wrapT"`
}

type accessor struct {
	BufferView    int       `json:"bufferView"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Min           []float64 `json:"min,omitempty"`
	Max           []float64 `json:"max,omitempty"`
}

type bufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	Target     int `json:"target,omitempty"`
}

type buffer struct {
	ByteLength int `json:"byteLength"`
}

const (
	glFloat        = 5126
	glUnsignedInt  = 5125
	glArrayBuffer  = 34962
	glElementArray = 34963
	glLinear       = 9729
	glLinearMipmap = 9987 // LINEAR_MIPMAP_LINEAR
	glClampToEdge  = 33071
)

// The joints' nodes, in the order robot3d's Part.Joint names them.
var joints = []string{"base", "yaw", "head"}

// screenLift puts the screen's quad this far (mm) in front of the CoreS3's front.
const screenLift = 0.05

// builder collects the binary chunk and its views.
type builder struct {
	doc document
	bin bytes.Buffer
}

func (b *builder) view(data []byte, target int) int {
	for b.bin.Len()%4 != 0 {
		b.bin.WriteByte(0)
	}
	b.doc.BufferViews = append(b.doc.BufferViews, bufferView{ByteOffset: b.bin.Len(), ByteLength: len(data), Target: target})
	b.bin.Write(data)
	return len(b.doc.BufferViews) - 1
}

func (b *builder) accessor(a accessor) int {
	b.doc.Accessors = append(b.doc.Accessors, a)
	return len(b.doc.Accessors) - 1
}

// vec3s writes float32 triples; with bounds, it gives min and max (positions need them).
func (b *builder) vec3s(vs []robot3d.V3, bounds bool) int {
	buf := make([]byte, 0, 12*len(vs))
	lo := []float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi := []float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, v := range vs {
		for k, c := range [3]float64{v.X, v.Y, v.Z} {
			f := float32(c)
			buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(f))
			lo[k], hi[k] = math.Min(lo[k], float64(f)), math.Max(hi[k], float64(f))
		}
	}
	a := accessor{BufferView: b.view(buf, glArrayBuffer), ComponentType: glFloat, Count: len(vs), Type: "VEC3"}
	if bounds {
		a.Min, a.Max = lo, hi
	}
	return b.accessor(a)
}

func (b *builder) node(n node) int {
	b.doc.Nodes = append(b.doc.Nodes, n)
	return len(b.doc.Nodes) - 1
}

func (b *builder) child(parent, n int) {
	b.doc.Nodes[parent].Children = append(b.doc.Nodes[parent].Children, n)
}

// primitive adds an indexed triangle list (the same position, normal and UV merged into one vertex).
func (b *builder) primitive(positions, normals []robot3d.V3, uvs [][2]float32, mat int) primitive {
	type key struct {
		p, n [3]float32
		uv   [2]float32
	}
	seen := map[key]uint32{}
	var pos, nrm []robot3d.V3
	var uv [][2]float32
	idx := make([]byte, 0, 4*len(positions))
	for i, p := range positions {
		n := normals[i]
		k := key{p: [3]float32{float32(p.X), float32(p.Y), float32(p.Z)}, n: [3]float32{float32(n.X), float32(n.Y), float32(n.Z)}}
		if uvs != nil {
			k.uv = uvs[i]
		}
		j, ok := seen[k]
		if !ok {
			j = uint32(len(pos))
			seen[k] = j
			pos, nrm = append(pos, p), append(nrm, n)
			if uvs != nil {
				uv = append(uv, uvs[i])
			}
		}
		idx = binary.LittleEndian.AppendUint32(idx, j)
	}
	pr := primitive{Attributes: map[string]int{"POSITION": b.vec3s(pos, true), "NORMAL": b.vec3s(nrm, false)}, Material: mat}
	if uvs != nil {
		buf := make([]byte, 0, 8*len(uv))
		for _, t := range uv {
			buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(t[0]))
			buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(t[1]))
		}
		pr.Attributes["TEXCOORD_0"] = b.accessor(accessor{BufferView: b.view(buf, glArrayBuffer), ComponentType: glFloat, Count: len(uv), Type: "VEC2"})
	}
	pr.Indices = b.accessor(accessor{BufferView: b.view(idx, glElementArray), ComponentType: glUnsignedInt, Count: len(positions), Type: "SCALAR"})
	return pr
}

func (b *builder) mesh(name string, pr primitive) int {
	b.doc.Meshes = append(b.doc.Meshes, mesh{Name: name, Primitives: []primitive{pr}})
	return len(b.doc.Meshes) - 1
}

// texture adds a PNG as an image and a texture (sampler 0: linear, mipmapped, clamped).
func (b *builder) texture(pngBytes []byte) int {
	if len(b.doc.Samplers) == 0 {
		b.doc.Samplers = []sampler{{MagFilter: glLinear, MinFilter: glLinearMipmap, WrapS: glClampToEdge, WrapT: glClampToEdge}}
	}
	b.doc.Images = append(b.doc.Images, gltfImage{BufferView: b.view(pngBytes, 0), MimeType: "image/png"})
	b.doc.Textures = append(b.doc.Textures, texture{Sampler: 0, Source: len(b.doc.Images) - 1})
	return len(b.doc.Textures) - 1
}

// material adds a material, double-sided: robot3d draws both sides of its triangles (thin
// parts, and triangles wound either way), so one-sided culling would drop some, e.g. the LED boards.
func (b *builder) material(m material) int {
	m.DoubleSided = true
	b.doc.Materials = append(b.doc.Materials, m)
	return len(b.doc.Materials) - 1
}

// orient winds each triangle counter-clockwise seen from the side its normals point to, as glTF
// wants: a triangle wound the other way is seen from its back, and a double-sided material then
// lights it with its normals turned inward (dark, as if a hole), e.g. the -X, -Y and -Z faces of
// robot3d's drawn boxes (the CoreS3, the LED bars and boards, the pitch servo).
func orient(positions, normals []robot3d.V3) ([]robot3d.V3, []robot3d.V3) {
	pos := append([]robot3d.V3(nil), positions...)
	nrm := append([]robot3d.V3(nil), normals...)
	for i := 0; i+2 < len(pos); i += 3 {
		face := pos[i+1].Sub(pos[i]).Cross(pos[i+2].Sub(pos[i]))
		if face.Dot(nrm[i].Add(nrm[i+1]).Add(nrm[i+2])) < 0 {
			pos[i+1], pos[i+2] = pos[i+2], pos[i+1]
			nrm[i+1], nrm[i+2] = nrm[i+2], nrm[i+1]
		}
	}
	return pos, nrm
}

// linear turns an 8-bit sRGB channel into glTF's linear colour factor.
func linear(c uint8) float64 {
	s := float64(c) / 255
	if s <= 0.04045 {
		return s / 12.92
	}
	return math.Pow((s+0.055)/1.055, 2.4)
}

// darkScreen is the picture when none is given: an unlit screen.
func darkScreen() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for i := range img.Pix {
		img.Pix[i] = 0x0d
		if i%4 == 3 {
			img.Pix[i] = 0xff
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// build gives the glTF binary of the parts: the joints as nodes, each part a node with its mesh
// under its joint, the screen a textured quad (centre, width, height) on the head. With surface
// (robot3d.Surface), the textured parts get their details as textures; nil: plain colours.
func build(parts []robot3d.Part, pivot, centre robot3d.V3, w, h float64, screenPNG []byte, surface surfaceFunc) ([]byte, error) {
	if len(screenPNG) == 0 {
		screenPNG = darkScreen()
	}
	if _, err := png.DecodeConfig(bytes.NewReader(screenPNG)); err != nil {
		return nil, fmt.Errorf("screen: %w", err)
	}
	b := &builder{}
	b.doc.Asset = asset{Version: "2.0", Generator: "i41eu-s3d cmd/s3dgen from robot3d (i41eu-assets)",
		Copyright: "StackChan structure: M5Stack (MIT); the rest: Apache-2.0"}
	b.doc.Extras = map[string]any{
		"units":      "the robot node scales millimetres to metres; below it, millimetres",
		"axes":       "Y up, the robot faces +Z; +yaw (about Y) turns the head to the robot's left (+X), +pitch lifts the front (the head node turns about X by -pitch)",
		"pitchPivot": []float64{pivot.X, pivot.Y, pivot.Z},
		"screen":     map[string]any{"centre": []float64{centre.X, centre.Y, centre.Z}, "width": w, "height": h, "pixels": []int{320, 240}},
	}

	root := b.node(node{Name: "robot", Scale: []float64{0.001, 0.001, 0.001}})
	jointNode := map[string]int{}
	parent := root
	for _, j := range joints {
		n := node{Name: j, Extras: map[string]any{"joint": j}}
		switch j {
		case "yaw":
			n.Extras["axis"] = "y"
		case "head":
			n.Translation = []float64{pivot.X, pivot.Y, pivot.Z}
			n.Extras["axis"] = "x"
			n.Extras["note"] = "at the pitch pivot: rotate by -pitch about X (+pitch lifts the front)"
		}
		k := b.node(n)
		b.child(parent, k)
		jointNode[j], parent = k, k
	}

	place := func(name, joint string, m int) error {
		jn, ok := jointNode[joint]
		if !ok {
			return fmt.Errorf("part %s: unknown joint %q", name, joint)
		}
		n := node{Name: name, Mesh: &m, Extras: map[string]any{"joint": joint}}
		if joint == "head" {
			n.Translation = []float64{-pivot.X, -pivot.Y, -pivot.Z}
		}
		b.child(jn, b.node(n))
		return nil
	}

	for _, p := range parts {
		if len(p.Positions) == 0 || len(p.Positions)%3 != 0 || len(p.Normals) != len(p.Positions) {
			return nil, fmt.Errorf("part %s: %d positions, %d normals", p.Name, len(p.Positions), len(p.Normals))
		}
		p.Positions, p.Normals = orient(p.Positions, p.Normals)
		if surface != nil && textured[p.Name] {
			// Each triangle on the face of the part's bounds it faces, its texels from surface.
			a := layout(bounds(p.Positions))
			uvs := make([][2]float32, len(p.Positions))
			for i := 0; i < len(p.Positions); i += 3 {
				t := [3]robot3d.V3{p.Positions[i], p.Positions[i+1], p.Positions[i+2]}
				k := a.face(t, [3]robot3d.V3{p.Normals[i], p.Normals[i+1], p.Normals[i+2]})
				for j := range t {
					uvs[i+j] = a.uv(k, t[j])
				}
			}
			base, rough := a.paint(p, surface)
			mat := b.material(material{Name: p.Name, PBRMetallicRoughness: pbr{
				BaseColorFactor: []float64{1, 1, 1, 1}, BaseColorTexture: &textureInfo{Index: b.texture(base)},
				MetallicRoughnessTexture: &textureInfo{Index: b.texture(rough)}, RoughnessFactor: 1,
			}})
			if err := place(p.Name, p.Joint, b.mesh(p.Name, b.primitive(p.Positions, p.Normals, uvs, mat))); err != nil {
				return nil, err
			}
			continue
		}
		mat := b.material(material{Name: p.Name, PBRMetallicRoughness: pbr{
			BaseColorFactor: []float64{linear(p.Color.R), linear(p.Color.G), linear(p.Color.B), 1},
			RoughnessFactor: roughShell,
		}})
		if err := place(p.Name, p.Joint, b.mesh(p.Name, b.primitive(p.Positions, p.Normals, nil, mat))); err != nil {
			return nil, err
		}
	}

	// The screen: the picture's top left at centre - (w/2, -h/2), facing +Z, unlit.
	b.doc.ExtensionsUsed = []string{"KHR_materials_unlit"}
	mat := b.material(material{Name: "screen", PBRMetallicRoughness: pbr{
		BaseColorFactor: []float64{1, 1, 1, 1}, BaseColorTexture: &textureInfo{Index: b.texture(screenPNG)}, RoughnessFactor: 1,
	}, Extensions: map[string]any{"KHR_materials_unlit": map[string]any{}}})
	z := centre.Z + screenLift
	tl := robot3d.V3{X: centre.X - w/2, Y: centre.Y + h/2, Z: z}
	tr := robot3d.V3{X: centre.X + w/2, Y: centre.Y + h/2, Z: z}
	br := robot3d.V3{X: centre.X + w/2, Y: centre.Y - h/2, Z: z}
	bl := robot3d.V3{X: centre.X - w/2, Y: centre.Y - h/2, Z: z}
	fwd := robot3d.V3{Z: 1}
	pos := []robot3d.V3{tl, bl, br, tl, br, tr} // counter-clockwise seen from the front
	uv := [][2]float32{{0, 0}, {0, 1}, {1, 1}, {0, 0}, {1, 1}, {1, 0}}
	if err := place("screen", "head", b.mesh("screen", b.primitive(pos, []robot3d.V3{fwd, fwd, fwd, fwd, fwd, fwd}, uv, mat))); err != nil {
		return nil, err
	}

	b.doc.Scenes = []scene{{Name: "robot", Nodes: []int{root}}}
	for b.bin.Len()%4 != 0 {
		b.bin.WriteByte(0)
	}
	b.doc.Buffers = []buffer{{ByteLength: b.bin.Len()}}
	js, err := json.Marshal(b.doc)
	if err != nil {
		return nil, err
	}
	for len(js)%4 != 0 {
		js = append(js, ' ')
	}

	var out bytes.Buffer
	le := func(v uint32) { binary.Write(&out, binary.LittleEndian, v) }
	le(0x46546C67) // "glTF"
	le(2)
	le(uint32(12 + 8 + len(js) + 8 + b.bin.Len()))
	le(uint32(len(js)))
	le(0x4E4F534A) // "JSON"
	out.Write(js)
	le(uint32(b.bin.Len()))
	le(0x004E4942) // "BIN\0"
	out.Write(b.bin.Bytes())
	return out.Bytes(), nil
}
