package threemf

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type file struct {
	name   string
	data   string
	stored bool // zip.Store instead of zip.Deflate
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/>
<Default Extension="png" ContentType="image/png"/>
<Default Extension="config" ContentType="text/xml"/>
<Default Extension="gcode" ContentType="text/x.gcode"/>
<Default Extension="bin" ContentType="application/octet-stream"/>
</Types>`

func rels(entries ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 0; i+1 < len(entries); i += 2 {
		fmt.Fprintf(&b, `<Relationship Id="r%d" Target="%s" Type="%s"/>`, i, entries[i], entries[i+1])
	}
	b.WriteString("</Relationships>")
	return b.String()
}

func startRels(target string) file {
	return file{name: "_rels/.rels", data: rels(target, RelTypeStartPart)}
}

func zipOf(t testing.TB, files ...file) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		m := zip.Deflate
		if f.stored {
			m = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.name, Method: m})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(f.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.SetComment("fixture comment"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pkgWith builds a package with content types, a StartPart rel to
// /3D/3dmodel.model holding model, plus extra files.
func pkgWith(t testing.TB, model string, extra ...file) []byte {
	t.Helper()
	files := []file{
		{name: "[Content_Types].xml", data: contentTypes},
		startRels("/3D/3dmodel.model"),
		{name: "3D/3dmodel.model", data: model},
	}
	return zipOf(t, append(files, extra...)...)
}

func load(t testing.TB, b []byte) *Document {
	t.Helper()
	d, err := loadErr(b, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func loadErr(b []byte, lim Limits) (*Document, error) {
	p, err := OpenBytes(b, lim)
	if err != nil {
		return nil, err
	}
	return Load(p)
}

func save(t testing.TB, d *Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := d.Save(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var cubeVerts = [][3]float64{
	{0, 0, 0}, {10, 0, 0}, {10, 10, 0}, {0, 10, 0},
	{0, 0, 10}, {10, 0, 10}, {10, 10, 10}, {0, 10, 10},
}

// cubeTris are outward-facing (counter-clockwise seen from outside).
var cubeTris = [][3]int{
	{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7},
	{0, 1, 5}, {0, 5, 4}, {3, 7, 6}, {3, 6, 2},
	{0, 4, 7}, {0, 7, 3}, {1, 2, 6}, {1, 6, 5},
}

// cubeMesh returns a <mesh> element; triAttr is appended to every triangle.
func cubeMesh(prefix, triAttr string) string {
	e := func(n string) string {
		if prefix == "" {
			return n
		}
		return prefix + ":" + n
	}
	var b strings.Builder
	b.WriteString("<" + e("mesh") + "><" + e("vertices") + ">")
	for _, v := range cubeVerts {
		fmt.Fprintf(&b, `<%s x="%g" y="%g" z="%g"/>`, e("vertex"), v[0], v[1], v[2])
	}
	b.WriteString("</" + e("vertices") + "><" + e("triangles") + ">")
	for _, t := range cubeTris {
		fmt.Fprintf(&b, `<%s v1="%d" v2="%d" v3="%d"%s/>`, e("triangle"), t[0], t[1], t[2], triAttr)
	}
	b.WriteString("</" + e("triangles") + "></" + e("mesh") + ">")
	return b.String()
}

const modelOpen = `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">`

func simpleModel(itemAttrs string) string {
	return modelOpen + `<resources><object id="1" type="model">` + cubeMesh("", "") +
		`</object></resources><build><item objectid="1"` + itemAttrs + `/></build></model>`
}

func mustErrIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want %v", err, target)
	}
}

func hasWarning(ws []Warning, code string) bool {
	for _, w := range ws {
		if w.Code == code {
			return true
		}
	}
	return false
}

func near(a, b [3]float64) bool {
	for i := range a {
		d := a[i] - b[i]
		if d > 1e-9 || d < -1e-9 {
			return false
		}
	}
	return true
}
