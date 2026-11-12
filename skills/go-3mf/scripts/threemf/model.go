package threemf

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// span is a half-open byte range [start, end) in a model part's original bytes.
type span struct{ start, end int64 }

// Metadata is a model-level <metadata> element. Name is the raw QName as
// written (for example "BambuStudio:3mfVersion").
type Metadata struct {
	Name     string
	Value    string
	Preserve bool
}

// ModelPart is one parsed 3D Model part. It is a read-only index over the
// original XML bytes: edits are recorded as byte splices (see edit.go), so
// everything the parser does not understand survives a save unchanged.
type ModelPart struct {
	Name               string
	Unit               string
	UnitScale          float64           // millimetres per model unit
	Namespaces         map[string]string // prefix -> URI declared on <model> ("" = default)
	RequiredExtensions []string          // namespace URIs
	Metadata           []Metadata
	Objects            []*Object
	BuildUUID          string
	Items              []*Item // only meaningful in the root part

	objects     map[int]*Object
	resourceIDs map[int]string
	data        []byte
}

// Object is an <object> resource. It has either a Mesh or Components.
type Object struct {
	ID         int
	Type       string // model, solidsupport, support, surface, other
	Name       string
	PartNumber string
	UUID       string // p:UUID
	PID        string // raw pid attribute, if any
	Mesh       *Mesh
	Components []*Component
	Order      int // position among the part's objects (forward-reference checks)
}

// Mesh holds geometry in the object's local space, in the part's unit.
type Mesh struct {
	Vertices  [][3]float64
	Triangles [][3]int

	// TriangleAttrs counts non-v1/v2/v3 triangle attributes by key ("p1",
	// "pid", "paint_color", "{namespace-uri}local"). These are data tied to
	// triangle indices; a topology edit cannot keep them meaningful.
	TriangleAttrs map[string]int
	// VertexAttrs counts non-x/y/z vertex attributes the same way.
	VertexAttrs map[string]int
	// ExtraChildren lists foreign elements inside <mesh>, such as
	// "{trianglesets-uri}trianglesets", which may reference triangle indices.
	ExtraChildren []string

	prefix         string // element prefix used for core mesh children ("" or "c")
	verticesSpan   span
	trianglesSpan  span
	sawVertices    bool
	sawTriangles   bool
	vertexElemName string
}

// Component references another object. Path is the resolved part name from
// p:path, or "" for the same part.
type Component struct {
	ObjectID     int
	Path         string
	Transform    Matrix
	HasTransform bool
	UUID         string
	tag          span
}

// Item is a <build><item>.
type Item struct {
	ObjectID     int
	Path         string
	Transform    Matrix
	HasTransform bool
	PartNumber   string
	UUID         string
	tag          span
}

// Object returns the object with the given id, or nil.
func (m *ModelPart) Object(id int) *Object { return m.objects[id] }

func attrKey(a xml.Name) string {
	if a.Space == "" {
		return a.Local
	}
	return "{" + a.Space + "}" + a.Local
}

// ParseModel parses a 3D Model part. triBudget, when non-nil, is decremented
// per triangle and parsing fails when it goes negative.
func ParseModel(name string, data []byte, triBudget *int) (*ModelPart, error) {
	mp := &ModelPart{
		Name:        name,
		Namespaces:  map[string]string{},
		objects:     map[int]*Object{},
		resourceIDs: map[int]string{},
		data:        data,
	}
	base := int64(0)
	if bytes.HasPrefix(data, []byte("\xEF\xBB\xBF")) {
		base = 3 // UTF-8 BOM; keep offsets relative to the original bytes
	}
	dec := xml.NewDecoder(bytes.NewReader(data[base:]))
	fail := func(format string, args ...any) error {
		line, col := dec.InputPos()
		return fmt.Errorf("%w: %s:%d:%d: %s", ErrMalformedModel, name, line, col, fmt.Sprintf(format, args...))
	}

	var (
		stack   []xml.Name
		obj     *Object
		mesh    *Mesh
		meta    *Metadata
		metaBuf strings.Builder
		sawRoot bool
	)
	parent := func() xml.Name {
		if len(stack) < 2 {
			return xml.Name{}
		}
		return stack[len(stack)-2]
	}
	for {
		start := dec.InputOffset() + base
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fail("%v", err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			if bytes.HasPrefix(bytes.TrimSpace(t), []byte("DOCTYPE")) {
				return nil, fail("DTD content is not allowed")
			}
		case xml.CharData:
			if meta != nil {
				metaBuf.Write(t)
			}
		case xml.StartElement:
			stack = append(stack, t.Name)
			end := dec.InputOffset() + base
			if len(stack) == 1 {
				if t.Name.Space != NSCore || t.Name.Local != "model" {
					return nil, fail("root element is {%s}%s, want core <model>", t.Name.Space, t.Name.Local)
				}
				sawRoot = true
				if err := mp.parseModelAttrs(t); err != nil {
					return nil, fail("%v", err)
				}
				continue
			}
			p := parent()
			if t.Name.Space != NSCore {
				if mesh != nil && p.Space == NSCore && p.Local == "mesh" {
					mesh.ExtraChildren = append(mesh.ExtraChildren, attrKey(t.Name))
				}
				continue
			}
			if p.Space != NSCore {
				continue // core-named element nested inside an extension element
			}
			switch t.Name.Local {
			case "metadata":
				if p.Local == "model" {
					meta = &Metadata{}
					metaBuf.Reset()
					for _, a := range t.Attr {
						switch {
						case a.Name.Space == "" && a.Name.Local == "name":
							meta.Name = a.Value
						case a.Name.Space == "" && a.Name.Local == "preserve":
							meta.Preserve = a.Value == "1" || a.Value == "true"
						}
					}
				}
			case "object":
				if p.Local != "resources" {
					return nil, fail("<object> outside <resources>")
				}
				o, err := parseObjectAttrs(t)
				if err != nil {
					return nil, fail("%v", err)
				}
				if prev, dup := mp.resourceIDs[o.ID]; dup {
					return nil, fail("duplicate resource id %d (already used by %s)", o.ID, prev)
				}
				o.Order = len(mp.Objects)
				mp.resourceIDs[o.ID] = "object"
				mp.objects[o.ID] = o
				mp.Objects = append(mp.Objects, o)
				obj = o
			case "basematerials":
				if p.Local == "resources" {
					id, err := parseResourceID(attr(t, "", "id"))
					if err != nil {
						return nil, fail("basematerials: %v", err)
					}
					if prev, dup := mp.resourceIDs[id]; dup {
						return nil, fail("duplicate resource id %d (already used by %s)", id, prev)
					}
					mp.resourceIDs[id] = "basematerials"
				}
			case "mesh":
				if obj == nil || p.Local != "object" {
					return nil, fail("<mesh> outside <object>")
				}
				if obj.Components != nil || obj.Mesh != nil {
					return nil, fail("object %d has more than one shape", obj.ID)
				}
				mesh = &Mesh{TriangleAttrs: map[string]int{}, VertexAttrs: map[string]int{}}
				obj.Mesh = mesh
			case "vertices":
				if mesh == nil || p.Local != "mesh" {
					return nil, fail("<vertices> outside <mesh>")
				}
				mesh.verticesSpan.start = start
				mesh.sawVertices = true
				mesh.prefix = rawPrefix(mp.data[start:end])
			case "vertex":
				if mesh == nil || p.Local != "vertices" {
					return nil, fail("<vertex> outside <vertices>")
				}
				v, err := parseVertex(t, mesh)
				if err != nil {
					return nil, fail("vertex %d: %v", len(mesh.Vertices), err)
				}
				mesh.Vertices = append(mesh.Vertices, v)
			case "triangles":
				if mesh == nil || p.Local != "mesh" {
					return nil, fail("<triangles> outside <mesh>")
				}
				mesh.trianglesSpan.start = start
				mesh.sawTriangles = true
			case "triangle":
				if mesh == nil || p.Local != "triangles" {
					return nil, fail("<triangle> outside <triangles>")
				}
				tri, err := parseTriangle(t, mesh, len(mesh.Vertices))
				if err != nil {
					return nil, fail("triangle %d: %v", len(mesh.Triangles), err)
				}
				mesh.Triangles = append(mesh.Triangles, tri)
				if triBudget != nil {
					*triBudget--
					if *triBudget < 0 {
						return nil, fmt.Errorf("%w: too many triangles in %s", ErrLimitExceeded, name)
					}
				}
			case "components":
				if obj == nil || p.Local != "object" {
					return nil, fail("<components> outside <object>")
				}
				if obj.Mesh != nil || obj.Components != nil {
					return nil, fail("object %d has more than one shape", obj.ID)
				}
				obj.Components = []*Component{}
			case "component":
				if obj == nil || p.Local != "components" {
					return nil, fail("<component> outside <components>")
				}
				c, err := parseComponent(t)
				if err != nil {
					return nil, fail("component: %v", err)
				}
				c.tag = span{start, end}
				obj.Components = append(obj.Components, c)
			case "build":
				if p.Local == "model" {
					mp.BuildUUID = attr(t, NSProduction, "UUID")
				}
			case "item":
				if p.Local != "build" {
					return nil, fail("<item> outside <build>")
				}
				it, err := parseItem(t)
				if err != nil {
					return nil, fail("item: %v", err)
				}
				it.tag = span{start, end}
				mp.Items = append(mp.Items, it)
			}
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fail("unbalanced end element")
			}
			stack = stack[:len(stack)-1]
			end := dec.InputOffset() + base
			if t.Name.Space != NSCore {
				continue
			}
			switch t.Name.Local {
			case "metadata":
				if meta != nil {
					meta.Value = metaBuf.String()
					mp.Metadata = append(mp.Metadata, *meta)
					meta = nil
				}
			case "object":
				obj, mesh = nil, nil
			case "vertices":
				if mesh != nil {
					mesh.verticesSpan.end = end
				}
			case "triangles":
				if mesh != nil {
					mesh.trianglesSpan.end = end
				}
			}
		}
	}
	if !sawRoot {
		return nil, fmt.Errorf("%w: %s: no <model> element", ErrMalformedModel, name)
	}
	for _, o := range mp.Objects {
		if o.Mesh != nil && (!o.Mesh.sawVertices || !o.Mesh.sawTriangles) {
			return nil, fmt.Errorf("%w: %s: object %d mesh lacks <vertices> or <triangles>", ErrMalformedModel, name, o.ID)
		}
	}
	return mp, nil
}

func (mp *ModelPart) parseModelAttrs(t xml.StartElement) error {
	mp.Unit = "millimeter"
	var required string
	for _, a := range t.Attr {
		switch {
		case a.Name.Space == "xmlns":
			mp.Namespaces[a.Name.Local] = a.Value
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			mp.Namespaces[""] = a.Value
		case a.Name.Space == "" && a.Name.Local == "unit":
			mp.Unit = strings.TrimSpace(a.Value)
		case a.Name.Space == "" && a.Name.Local == "requiredextensions":
			required = a.Value
		}
	}
	s, ok := UnitScale[mp.Unit]
	if !ok {
		return fmt.Errorf("unknown unit %q", mp.Unit)
	}
	mp.UnitScale = s
	for _, prefix := range strings.Fields(required) {
		uri, ok := mp.Namespaces[prefix]
		if !ok {
			return fmt.Errorf("required extension prefix %q is not declared on <model>", prefix)
		}
		mp.RequiredExtensions = append(mp.RequiredExtensions, uri)
	}
	return nil
}

func attr(t xml.StartElement, space, local string) string {
	for _, a := range t.Attr {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func hasAttr(t xml.StartElement, space, local string) bool {
	for _, a := range t.Attr {
		if a.Name.Space == space && a.Name.Local == local {
			return true
		}
	}
	return false
}

func parseObjectAttrs(t xml.StartElement) (*Object, error) {
	id, err := parseResourceID(attr(t, "", "id"))
	if err != nil {
		return nil, fmt.Errorf("object: %w", err)
	}
	o := &Object{
		ID:         id,
		Type:       "model",
		Name:       attr(t, "", "name"),
		PartNumber: attr(t, "", "partnumber"),
		UUID:       attr(t, NSProduction, "UUID"),
		PID:        attr(t, "", "pid"),
	}
	if hasAttr(t, "", "type") {
		o.Type = strings.TrimSpace(attr(t, "", "type"))
	}
	switch o.Type {
	case "model", "solidsupport", "support", "surface", "other":
	default:
		return nil, fmt.Errorf("object %d: invalid type %q", id, o.Type)
	}
	return o, nil
}

func parseVertex(t xml.StartElement, mesh *Mesh) ([3]float64, error) {
	var v [3]float64
	var seen [3]bool
	for _, a := range t.Attr {
		i := -1
		if a.Name.Space == "" {
			switch a.Name.Local {
			case "x":
				i = 0
			case "y":
				i = 1
			case "z":
				i = 2
			}
		}
		if i < 0 {
			mesh.VertexAttrs[attrKey(a.Name)]++
			continue
		}
		f, err := ParseNumber(a.Value)
		if err != nil {
			return v, err
		}
		v[i], seen[i] = f, true
	}
	if !seen[0] || !seen[1] || !seen[2] {
		return v, errors.New("missing x, y or z")
	}
	return v, nil
}

func parseTriangle(t xml.StartElement, mesh *Mesh, nVerts int) ([3]int, error) {
	var tri [3]int
	var seen [3]bool
	for _, a := range t.Attr {
		i := -1
		if a.Name.Space == "" {
			switch a.Name.Local {
			case "v1":
				i = 0
			case "v2":
				i = 1
			case "v3":
				i = 2
			}
		}
		if i < 0 {
			mesh.TriangleAttrs[attrKey(a.Name)]++
			continue
		}
		v, err := parseIndex(a.Value)
		if err != nil {
			return tri, err
		}
		// Vertices must precede triangles (forward-only parsing, Core 4.1).
		if v >= nVerts {
			return tri, fmt.Errorf("%w: vertex index %d out of range (%d vertices)", ErrMalformedModel, v, nVerts)
		}
		tri[i], seen[i] = v, true
	}
	if !seen[0] || !seen[1] || !seen[2] {
		return tri, errors.New("missing v1, v2 or v3")
	}
	return tri, nil
}

func parseComponent(t xml.StartElement) (*Component, error) {
	id, err := parseResourceID(attr(t, "", "objectid"))
	if err != nil {
		return nil, err
	}
	c := &Component{ObjectID: id, UUID: attr(t, NSProduction, "UUID")}
	if c.HasTransform = hasAttr(t, "", "transform"); c.HasTransform {
		if c.Transform, err = ParseMatrix(attr(t, "", "transform")); err != nil {
			return nil, err
		}
	} else {
		c.Transform = Identity()
	}
	if hasAttr(t, NSProduction, "path") {
		if c.Path, err = normalizePartRef(attr(t, NSProduction, "path")); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func parseItem(t xml.StartElement) (*Item, error) {
	id, err := parseResourceID(attr(t, "", "objectid"))
	if err != nil {
		return nil, err
	}
	it := &Item{ObjectID: id, PartNumber: attr(t, "", "partnumber"), UUID: attr(t, NSProduction, "UUID")}
	if it.HasTransform = hasAttr(t, "", "transform"); it.HasTransform {
		if it.Transform, err = ParseMatrix(attr(t, "", "transform")); err != nil {
			return nil, err
		}
	} else {
		it.Transform = Identity()
	}
	if hasAttr(t, NSProduction, "path") {
		if it.Path, err = normalizePartRef(attr(t, NSProduction, "path")); err != nil {
			return nil, err
		}
	}
	return it, nil
}

// rawPrefix returns the namespace prefix written in a raw start tag
// ("<c:vertices ..." -> "c").
func rawPrefix(tag []byte) string {
	name := tag[1:]
	if i := bytes.IndexAny(name, " \t\r\n/>"); i >= 0 {
		name = name[:i]
	}
	if i := bytes.IndexByte(name, ':'); i >= 0 {
		return string(name[:i])
	}
	return ""
}
