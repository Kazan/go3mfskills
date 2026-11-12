package threemf

import (
	"archive/zip"
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// MeshObject is one object to generate. Vertices are in GenerateOptions.Unit.
type MeshObject struct {
	Name       string
	PartNumber string
	Vertices   [][3]float64
	Triangles  [][3]int
	// Transform places the object's build item; nil writes no transform.
	Transform *Matrix
}

// GenerateOptions configures a new package.
type GenerateOptions struct {
	Unit     string     // ST_Unit; default "millimeter"
	Lang     string     // xml:lang; default "en-US"
	Metadata []Metadata // names must be well-known or prefixed
	// ProductionUUIDs adds the production namespace, requiredextensions="p"
	// and p:UUID on build, items and objects, as Bambu Studio writes them.
	ProductionUUIDs bool
	// Thumbnail is an optional PNG stored at /Metadata/thumbnail.png and
	// linked from the package relationships.
	Thumbnail []byte
	// SkipMeshCheck writes meshes that fail CheckMesh. Leave false: Core
	// requires model meshes to be closed and consistently oriented.
	SkipMeshCheck bool
}

// Generate writes a new single-model 3MF package. It validates everything it
// writes; the output is Core-conformant when it returns nil.
func Generate(w io.Writer, objs []MeshObject, opt GenerateOptions) error {
	if len(objs) == 0 {
		return errors.New("threemf: no objects to generate")
	}
	if opt.Unit == "" {
		opt.Unit = "millimeter"
	}
	if _, ok := UnitScale[opt.Unit]; !ok {
		return fmt.Errorf("threemf: unknown unit %q", opt.Unit)
	}
	if opt.Lang == "" {
		opt.Lang = "en-US"
	}
	seen := map[string]bool{}
	for _, m := range opt.Metadata {
		if seen[m.Name] {
			return fmt.Errorf("threemf: duplicate metadata %q", m.Name)
		}
		seen[m.Name] = true
		if !strings.Contains(m.Name, ":") && !WellKnownMetadata[m.Name] {
			return fmt.Errorf("threemf: metadata %q must be a well-known name or namespace-prefixed", m.Name)
		}
		if prefix, _, ok := strings.Cut(m.Name, ":"); ok && prefix != "p" {
			return fmt.Errorf("threemf: metadata prefix %q is not declared; extend Generate to declare it", prefix)
		}
	}
	for i, o := range objs {
		if err := checkFinite(o.Vertices); err != nil {
			return fmt.Errorf("object %d: %w", i, err)
		}
		if !opt.SkipMeshCheck {
			if probs := CheckMesh(o.Vertices, o.Triangles); len(probs) > 0 {
				return fmt.Errorf("threemf: object %d (%s): %s", i, o.Name, strings.Join(probs, "; "))
			}
		}
		if o.Transform != nil {
			for _, v := range o.Transform {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return fmt.Errorf("threemf: object %d transform is not finite", i)
				}
			}
		}
	}

	zw := zip.NewWriter(w)
	put := func(name string, fn func(*bufio.Writer) error) error {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		bw := bufio.NewWriterSize(fw, 1<<16)
		if err := fn(bw); err != nil {
			return err
		}
		return bw.Flush()
	}
	str := func(s string) func(*bufio.Writer) error {
		return func(b *bufio.Writer) error { _, err := b.WriteString(s); return err }
	}

	ct := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Types xmlns="` + NSContentTypes + `">` + "\n" +
		`<Default Extension="rels" ContentType="` + ContentTypeRels + `"/>` + "\n" +
		`<Default Extension="model" ContentType="` + ContentTypeModel + `"/>` + "\n"
	if opt.Thumbnail != nil {
		ct += `<Default Extension="png" ContentType="` + ContentTypePNG + `"/>` + "\n"
	}
	ct += "</Types>\n"
	rels := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Relationships xmlns="` + NSRelationships + `">` + "\n" +
		`<Relationship Id="rel0" Target="/3D/3dmodel.model" Type="` + RelTypeStartPart + `"/>` + "\n"
	if opt.Thumbnail != nil {
		rels += `<Relationship Id="rel1" Target="/Metadata/thumbnail.png" Type="` + RelTypeThumbnail + `"/>` + "\n"
	}
	rels += "</Relationships>\n"

	if err := put("[Content_Types].xml", str(ct)); err != nil {
		return err
	}
	if err := put("_rels/.rels", str(rels)); err != nil {
		return err
	}
	if err := put("3D/3dmodel.model", func(b *bufio.Writer) error { return writeModel(b, objs, opt) }); err != nil {
		return err
	}
	if opt.Thumbnail != nil {
		if err := put("Metadata/thumbnail.png", func(b *bufio.Writer) error { _, err := b.Write(opt.Thumbnail); return err }); err != nil {
			return err
		}
	}
	return zw.Close()
}

func writeModel(b *bufio.Writer, objs []MeshObject, opt GenerateOptions) error {
	uuid := func() (string, error) {
		if !opt.ProductionUUIDs {
			return "", nil
		}
		u, err := NewUUID()
		return ` p:UUID="` + u + `"`, err
	}
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<model unit="` + opt.Unit + `" xml:lang="` + escapeAttr(opt.Lang) + `" xmlns="` + NSCore + `"`)
	if opt.ProductionUUIDs {
		b.WriteString(` xmlns:p="` + NSProduction + `" requiredextensions="p"`)
	}
	b.WriteString(">\n")
	for _, m := range opt.Metadata {
		b.WriteString(`<metadata name="` + escapeAttr(m.Name) + `"`)
		if m.Preserve {
			b.WriteString(` preserve="1"`)
		}
		b.WriteString(">" + escapeText(m.Value) + "</metadata>\n")
	}
	b.WriteString("<resources>\n")
	for i, o := range objs {
		u, err := uuid()
		if err != nil {
			return err
		}
		b.WriteString(`<object id="` + strconv.Itoa(i+1) + `" type="model"`)
		if o.Name != "" {
			b.WriteString(` name="` + escapeAttr(o.Name) + `"`)
		}
		if o.PartNumber != "" {
			b.WriteString(` partnumber="` + escapeAttr(o.PartNumber) + `"`)
		}
		b.WriteString(u + ">\n<mesh>\n")
		b.Write(writeVertices("", o.Vertices))
		b.WriteString("\n")
		b.Write(writeTriangles("", o.Triangles))
		b.WriteString("\n</mesh>\n</object>\n")
	}
	b.WriteString("</resources>\n")
	u, err := uuid()
	if err != nil {
		return err
	}
	b.WriteString("<build" + u + ">\n")
	for i, o := range objs {
		u, err := uuid()
		if err != nil {
			return err
		}
		b.WriteString(`<item objectid="` + strconv.Itoa(i+1) + `"`)
		if o.Transform != nil {
			b.WriteString(` transform="` + o.Transform.String() + `"`)
		}
		if o.PartNumber != "" {
			b.WriteString(` partnumber="` + escapeAttr(o.PartNumber) + `"`)
		}
		b.WriteString(u + "/>\n")
	}
	_, err = b.WriteString("</build>\n</model>\n")
	return err
}

func escapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#xD;")
	return r.Replace(s)
}

// NewUUID returns a random RFC 4122 version 4 UUID, the form ST_UUID expects.
func NewUUID() (string, error) {
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		return "", err
	}
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16]), nil
}
