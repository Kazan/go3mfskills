package threemf

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// splice replaces original bytes [start, end) of a model part with repl.
// Edits are kept against the original bytes, so every byte outside the
// edited elements (unknown namespaces, Bambu attributes, comments,
// formatting) is written back unchanged.
type splice struct {
	start, end int64
	repl       []byte
}

func (d *Document) addSplice(part string, s splice) error {
	key := strings.ToLower(part)
	list := d.splices[key]
	for i, o := range list {
		if o.start == s.start && o.end == s.end {
			list[i] = s // a later edit of the same element wins
			return nil
		}
		if s.start < o.end && o.start < s.end {
			return fmt.Errorf("threemf: overlapping edits in %s", part)
		}
	}
	d.splices[key] = append(list, s)
	return nil
}

// Modified reports whether any model part has pending edits.
func (d *Document) Modified() bool { return len(d.splices) > 0 }

// render returns the edited bytes of a model part, or nil if unchanged.
func (d *Document) render(part string) []byte {
	list := d.splices[strings.ToLower(part)]
	if len(list) == 0 {
		return nil
	}
	mp := d.Part(part)
	sort.Slice(list, func(i, j int) bool { return list[i].start < list[j].start })
	var b bytes.Buffer
	pos := int64(0)
	for _, s := range list {
		b.Write(mp.data[pos:s.start])
		b.Write(s.repl)
		pos = s.end
	}
	b.Write(mp.data[pos:])
	return b.Bytes()
}

func (d *Document) meshObject(part string, id int) (*ModelPart, *Object, error) {
	mp := d.Part(part)
	if mp == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrPartNotFound, part)
	}
	o := mp.Object(id)
	if o == nil {
		return nil, nil, fmt.Errorf("%w: %s has no object %d", ErrInvalidReference, part, id)
	}
	if o.Mesh == nil {
		return nil, nil, fmt.Errorf("%w: %s object %d is a components object, not a mesh", ErrInvalidReference, part, id)
	}
	return mp, o, nil
}

// SetVertices moves the vertices of a mesh object. The count must not change,
// so triangles, triangle-indexed paint and property references keep their
// meaning. Coordinates are object-local, in the part's unit.
func (d *Document) SetVertices(part string, objectID int, verts [][3]float64) ([]Warning, error) {
	mp, o, err := d.meshObject(part, objectID)
	if err != nil {
		return nil, err
	}
	if len(verts) != len(o.Mesh.Vertices) {
		return nil, fmt.Errorf("threemf: SetVertices needs %d vertices, got %d; use ReplaceMesh to change topology", len(o.Mesh.Vertices), len(verts))
	}
	if err := checkFinite(verts); err != nil {
		return nil, err
	}
	if err := d.addSplice(mp.Name, splice{o.Mesh.verticesSpan.start, o.Mesh.verticesSpan.end, writeVertices(o.Mesh.prefix, verts)}); err != nil {
		return nil, err
	}
	o.Mesh.Vertices = append([][3]float64(nil), verts...)

	warns := d.sharedWarnings(mp.Name, o)
	if len(o.Mesh.TriangleAttrs) > 0 {
		warns = append(warns, Warning{"triangle-attributes-unverified", mp.Name, fmt.Sprintf(
			"object %d: triangle attributes %s were kept by index; paint and properties now sit on moved geometry", o.ID, keys(o.Mesh.TriangleAttrs))})
	}
	warns = append(warns, d.vertexAttrWarning(mp, o)...)
	warns = append(warns, d.packageStaleWarnings()...)
	return warns, nil
}

// ReplaceMesh replaces an object's vertices and triangles. Triangle-level
// attributes (p1/p2/p3/pid, Bambu paint_*) cannot be mapped to new topology,
// so they are dropped and reported; every other byte of the part survives.
func (d *Document) ReplaceMesh(part string, objectID int, verts [][3]float64, tris [][3]int) ([]Warning, error) {
	mp, o, err := d.meshObject(part, objectID)
	if err != nil {
		return nil, err
	}
	if err := checkFinite(verts); err != nil {
		return nil, err
	}
	if len(verts) >= 1<<31 || len(tris) >= 1<<31 {
		return nil, fmt.Errorf("%w: mesh too large", ErrLimitExceeded)
	}
	for i, t := range tris {
		for _, v := range t {
			if v < 0 || v >= len(verts) {
				return nil, fmt.Errorf("threemf: triangle %d: vertex index %d out of range", i, v)
			}
		}
		if t[0] == t[1] || t[1] == t[2] || t[0] == t[2] {
			return nil, fmt.Errorf("threemf: triangle %d repeats a vertex (Core 4.1.4)", i)
		}
	}
	m := o.Mesh
	if err := d.addSplice(mp.Name, splice{m.verticesSpan.start, m.verticesSpan.end, writeVertices(m.prefix, verts)}); err != nil {
		return nil, err
	}
	if err := d.addSplice(mp.Name, splice{m.trianglesSpan.start, m.trianglesSpan.end, writeTriangles(m.prefix, tris)}); err != nil {
		return nil, err
	}

	warns := d.sharedWarnings(mp.Name, o)
	if len(m.TriangleAttrs) > 0 {
		warns = append(warns, Warning{"triangle-attributes-dropped", mp.Name, fmt.Sprintf(
			"object %d: triangle attributes %s were removed because topology changed (Bambu paint, materials)", o.ID, keys(m.TriangleAttrs))})
	}
	warns = append(warns, d.vertexAttrWarning(mp, o)...)
	if len(m.ExtraChildren) > 0 {
		warns = append(warns, Warning{"mesh-extension-stale", mp.Name, fmt.Sprintf(
			"object %d: mesh children %v were kept but may reference old triangle indices", o.ID, m.ExtraChildren)})
	}
	if o.Type == "model" && len(tris) < 4 {
		warns = append(warns, Warning{"mesh-too-small", mp.Name, fmt.Sprintf("object %d: a model mesh needs at least 4 triangles", o.ID)})
	}
	warns = append(warns, d.packageStaleWarnings()...)

	m.Vertices = append([][3]float64(nil), verts...)
	m.Triangles = append([][3]int(nil), tris...)
	m.TriangleAttrs, m.VertexAttrs = map[string]int{}, map[string]int{}
	return warns, nil
}

// SetItemTransform replaces (or adds) the transform of root build item i.
// The translation is in the root part's unit.
func (d *Document) SetItemTransform(i int, m Matrix) ([]Warning, error) {
	if i < 0 || i >= len(d.Root.Items) {
		return nil, fmt.Errorf("threemf: no build item %d", i)
	}
	it := d.Root.Items[i]
	if err := d.spliceTransform(d.Root, it.tag, m); err != nil {
		return nil, err
	}
	it.Transform, it.HasTransform = m, true
	return d.packageStaleWarnings(), nil
}

// SetComponentTransform replaces (or adds) the transform of component ci of
// a components object. The translation is in that part's unit.
func (d *Document) SetComponentTransform(part string, objectID, ci int, m Matrix) ([]Warning, error) {
	mp := d.Part(part)
	if mp == nil {
		return nil, fmt.Errorf("%w: %s", ErrPartNotFound, part)
	}
	o := mp.Object(objectID)
	if o == nil || ci < 0 || ci >= len(o.Components) {
		return nil, fmt.Errorf("%w: %s object %d has no component %d", ErrInvalidReference, part, objectID, ci)
	}
	c := o.Components[ci]
	if err := d.spliceTransform(mp, c.tag, m); err != nil {
		return nil, err
	}
	c.Transform, c.HasTransform = m, true
	return append(d.sharedWarnings(mp.Name, o), d.packageStaleWarnings()...), nil
}

func (d *Document) spliceTransform(mp *ModelPart, tag span, m Matrix) error {
	for _, v := range m {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("threemf: transform has a non-finite value")
		}
	}
	repl, err := setStartTagAttr(mp.data[tag.start:tag.end], "transform", m.String())
	if err != nil {
		return fmt.Errorf("%s: %w", mp.Name, err)
	}
	return d.addSplice(mp.Name, splice{tag.start, tag.end, repl})
}

// setStartTagAttr rewrites one unprefixed attribute in a raw start tag,
// appending it when absent. Other attributes keep their bytes and order.
func setStartTagAttr(tag []byte, name, value string) ([]byte, error) {
	i := 1
	for i < len(tag) && !isSpace(tag[i]) && tag[i] != '/' && tag[i] != '>' {
		i++ // element name
	}
	for {
		for i < len(tag) && isSpace(tag[i]) {
			i++
		}
		if i >= len(tag) {
			return nil, errors.New("unterminated start tag")
		}
		if tag[i] == '/' || tag[i] == '>' {
			// Not found: insert before "/>" or ">".
			var b bytes.Buffer
			b.Write(tag[:i])
			b.WriteString(" " + name + `="` + escapeAttr(value) + `"`)
			b.Write(tag[i:])
			return b.Bytes(), nil
		}
		ns := i
		for i < len(tag) && tag[i] != '=' && !isSpace(tag[i]) {
			i++
		}
		attrName := string(tag[ns:i])
		for i < len(tag) && (isSpace(tag[i]) || tag[i] == '=') {
			i++
		}
		if i >= len(tag) || (tag[i] != '"' && tag[i] != '\'') {
			return nil, errors.New("malformed attribute in start tag")
		}
		q := tag[i]
		vs := i + 1
		ve := bytes.IndexByte(tag[vs:], q)
		if ve < 0 {
			return nil, errors.New("unterminated attribute value")
		}
		ve += vs
		if attrName == name {
			var b bytes.Buffer
			b.Write(tag[:vs])
			b.WriteString(escapeAttr(value))
			b.Write(tag[ve:])
			return b.Bytes(), nil
		}
		i = ve + 1
	}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func escapeAttr(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		case '\t':
			b.WriteString("&#x9;")
		case '\n':
			b.WriteString("&#xA;")
		case '\r':
			b.WriteString("&#xD;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func checkFinite(verts [][3]float64) error {
	for i, v := range verts {
		for _, c := range v {
			if math.IsNaN(c) || math.IsInf(c, 0) {
				return fmt.Errorf("threemf: vertex %d has a non-finite coordinate", i)
			}
		}
	}
	return nil
}

func elem(prefix, local string) string {
	if prefix == "" {
		return local
	}
	return prefix + ":" + local
}

func writeVertices(prefix string, verts [][3]float64) []byte {
	var b bytes.Buffer
	b.Grow(len(verts) * 48)
	b.WriteString("<" + elem(prefix, "vertices") + ">\n")
	tag := "<" + elem(prefix, "vertex")
	for _, v := range verts {
		b.WriteString(tag)
		b.WriteString(` x="` + FormatNumber(v[0]) + `" y="` + FormatNumber(v[1]) + `" z="` + FormatNumber(v[2]) + "\"/>\n")
	}
	b.WriteString("</" + elem(prefix, "vertices") + ">")
	return b.Bytes()
}

func writeTriangles(prefix string, tris [][3]int) []byte {
	var b bytes.Buffer
	b.Grow(len(tris) * 40)
	b.WriteString("<" + elem(prefix, "triangles") + ">\n")
	tag := "<" + elem(prefix, "triangle")
	for _, t := range tris {
		b.WriteString(tag)
		b.WriteString(` v1="` + strconv.Itoa(t[0]) + `" v2="` + strconv.Itoa(t[1]) + `" v3="` + strconv.Itoa(t[2]) + "\"/>\n")
	}
	b.WriteString("</" + elem(prefix, "triangles") + ">")
	return b.Bytes()
}

func keys(m map[string]int) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ", ")
}

func (d *Document) sharedWarnings(part string, o *Object) []Warning {
	n, err := d.References(part, o.ID)
	if err != nil || n <= 1 {
		return nil
	}
	return []Warning{{"shared-object", part, fmt.Sprintf("object %d is placed %d times; the edit changes every instance", o.ID, n)}}
}

func (d *Document) vertexAttrWarning(mp *ModelPart, o *Object) []Warning {
	if len(o.Mesh.VertexAttrs) == 0 {
		return nil
	}
	return []Warning{{"vertex-attributes-dropped", mp.Name, fmt.Sprintf(
		"object %d: vertex attributes %s were not rewritten", o.ID, keys(o.Mesh.VertexAttrs))}}
}

// packageStaleWarnings names package content derived from geometry that this
// library keeps byte-for-byte but cannot regenerate. Nothing is deleted.
func (d *Document) packageStaleWarnings() []Warning {
	var w []Warning
	var plates, thumbs []string
	for _, e := range d.Pkg.Entries() {
		if e.IsDir {
			continue
		}
		n := strings.ToLower(e.Name)
		switch {
		case n == "/metadata/model_settings.config":
			w = append(w, Warning{"bambu-model-settings-stale", e.Name,
				"Bambu per-object settings (face_count, part matrix, source offsets, plate assignment) may no longer match the mesh"})
		case n == "/metadata/slice_info.config":
			w = append(w, Warning{"slice-info-stale", e.Name, "slicing results describe the old geometry"})
		case strings.HasPrefix(n, "/metadata/plate_") || strings.HasSuffix(n, ".gcode") || strings.HasSuffix(n, ".gcode.md5"):
			plates = append(plates, e.Name)
		case strings.HasPrefix(n, "/auxiliaries/.thumbnails/") || n == "/metadata/thumbnail.png":
			thumbs = append(thumbs, e.Name)
		}
	}
	for _, rels := range d.Pkg.AllRels() {
		for _, r := range rels {
			switch r.Type {
			case RelTypeThumbnail:
				thumbs = append(thumbs, r.Target)
			case RelTypeSignatureOrigin:
				w = append(w, Warning{"signature-invalidated", r.Target, "digital signatures over edited parts no longer verify"})
			}
		}
	}
	if len(plates) > 0 {
		w = append(w, Warning{"derived-artifacts-stale", "", fmt.Sprintf("plate previews/G-code are from the old geometry: %s", strings.Join(dedupe(plates), ", "))})
	}
	if len(thumbs) > 0 {
		w = append(w, Warning{"thumbnails-stale", "", fmt.Sprintf("thumbnails show the old geometry: %s", strings.Join(dedupe(thumbs), ", "))})
	}
	return w
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range s {
		if k := strings.ToLower(v); !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
