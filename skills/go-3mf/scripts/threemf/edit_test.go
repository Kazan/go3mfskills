package threemf

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rawEntries(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		r, err := f.OpenRaw()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = raw
	}
	return out
}

func entryNames(t *testing.T, b []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func partBytes(t *testing.T, b []byte, part string) []byte {
	t.Helper()
	p, err := OpenBytes(b, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.Read(part)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSetVerticesPreservesPackage(t *testing.T) {
	orig := bambuLike(t)
	d := load(t, orig)
	const sub = "/3D/Objects/object_1.model"
	moved := make([][3]float64, len(cubeVerts))
	for i, v := range cubeVerts {
		moved[i] = [3]float64{v[0] * 1.5, v[1], v[2] + 0.125}
	}
	warns, err := d.SetVertices(sub, 1, moved)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"triangle-attributes-unverified", "bambu-model-settings-stale", "thumbnails-stale", "derived-artifacts-stale"} {
		if !hasWarning(warns, code) {
			t.Errorf("missing warning %s in %v", code, warns)
		}
	}
	out := save(t, d)

	if got, want := entryNames(t, out), entryNames(t, orig); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entry order changed:\n%v\n%v", got, want)
	}
	before, after := rawEntries(t, orig), rawEntries(t, out)
	for name, raw := range before {
		if name == "3D/Objects/object_1.model" {
			continue
		}
		if !bytes.Equal(raw, after[name]) {
			t.Errorf("%s: compressed bytes changed", name)
		}
	}
	zr, _ := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if zr.Comment != "fixture comment" {
		t.Errorf("archive comment = %q", zr.Comment)
	}

	// Outside the <vertices> element the part is byte-identical.
	ob, nb := partBytes(t, orig, sub), partBytes(t, out, sub)
	s := bytes.Index(ob, []byte("<vertices>"))
	e := bytes.Index(ob, []byte("</vertices>")) + len("</vertices>")
	ns := bytes.Index(nb, []byte("<vertices>"))
	ne := bytes.Index(nb, []byte("</vertices>")) + len("</vertices>")
	if !bytes.Equal(ob[:s], nb[:ns]) || !bytes.Equal(ob[e:], nb[ne:]) {
		t.Fatal("bytes outside <vertices> changed")
	}

	d2 := load(t, out)
	m := d2.Part(sub).Object(1).Mesh
	for i := range moved {
		if m.Vertices[i] != moved[i] {
			t.Fatalf("vertex %d = %v, want %v (exact round trip)", i, m.Vertices[i], moved[i])
		}
	}
	if m.TriangleAttrs["paint_color"] != 12 {
		t.Fatal("paint attributes lost")
	}
}

func TestReplaceMeshWarnsAndDropsTriangleAttrs(t *testing.T) {
	model := modelOpen[:len(modelOpen)-1] + ` xmlns:t="http://schemas.microsoft.com/3dmanufacturing/trianglesets/2021/07" xmlns:x="urn:vendor">
<resources><object id="1" x:keep="yes">` + strings.Replace(cubeMesh("", ` pid="1" p1="0"`), "</mesh>",
		`<t:trianglesets><t:triangleset name="a" identifier="a"><t:ref index="3"/></t:triangleset></t:trianglesets></mesh>`, 1) +
		`</object></resources><build><item objectid="1"/><item objectid="1" transform="1 0 0 0 1 0 0 0 1 20 0 0"/></build></model>`
	d := load(t, pkgWith(t, model))
	tris := append([][3]int(nil), cubeTris...)
	verts := append([][3]float64(nil), cubeVerts...)
	verts = append(verts, [3]float64{5, 5, 12})
	// Replace the top face with a 4-triangle pyramid.
	tris[2], tris[3] = [3]int{4, 5, 8}, [3]int{5, 6, 8}
	tris = append(tris, [3]int{6, 7, 8}, [3]int{7, 4, 8})
	warns, err := d.ReplaceMesh(d.Root.Name, 1, verts, tris)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"shared-object", "triangle-attributes-dropped", "mesh-extension-stale"} {
		if !hasWarning(warns, code) {
			t.Errorf("missing warning %s in %v", code, warns)
		}
	}
	out := save(t, d)
	data := string(partBytes(t, out, "/3D/3dmodel.model"))
	for _, keep := range []string{`x:keep="yes"`, `<t:ref index="3"/>`, `xmlns:x="urn:vendor"`} {
		if !strings.Contains(data, keep) {
			t.Errorf("lost %s", keep)
		}
	}
	d2 := load(t, out)
	if issues := d2.Validate(ProfileStrict); hasIssue(issues, "mesh") {
		t.Fatalf("edited mesh invalid: %v", issues)
	}
	if n := len(d2.Root.Object(1).Mesh.Triangles); n != 14 {
		t.Fatalf("triangles = %d", n)
	}
}

func TestReplaceMeshRejectsBadInput(t *testing.T) {
	d := load(t, pkgWith(t, simpleModel("")))
	if _, err := d.ReplaceMesh(d.Root.Name, 1, cubeVerts, [][3]int{{0, 1, 99}}); err == nil {
		t.Fatal("out-of-range index accepted")
	}
	if _, err := d.ReplaceMesh(d.Root.Name, 1, cubeVerts, [][3]int{{0, 0, 1}}); err == nil {
		t.Fatal("degenerate triangle accepted")
	}
	if _, err := d.SetVertices(d.Root.Name, 1, cubeVerts[:3]); err == nil {
		t.Fatal("vertex count change accepted by SetVertices")
	}
	if d.Modified() {
		t.Fatal("rejected edits must not be recorded")
	}
}

func TestSetItemTransform(t *testing.T) {
	model := strings.Replace(simpleModel(` partnumber='A&amp;B'`), `</build>`, `<item objectid="1" transform="1 0 0 0 1 0 0 0 1 0 0 0" partnumber="x"/></build>`, 1)
	d := load(t, pkgWith(t, model))
	m := Translation(1.5, -2, 0.25)
	if _, err := d.SetItemTransform(0, m); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SetItemTransform(1, m); err != nil {
		t.Fatal(err)
	}
	data := string(partBytes(t, save(t, d), "/3D/3dmodel.model"))
	want0 := `<item objectid="1" partnumber='A&amp;B' transform="1 0 0 0 1 0 0 0 1 1.5 -2 0.25"/>`
	want1 := `<item objectid="1" transform="1 0 0 0 1 0 0 0 1 1.5 -2 0.25" partnumber="x"/>`
	if !strings.Contains(data, want0) || !strings.Contains(data, want1) {
		t.Fatalf("unexpected items in:\n%s", data)
	}
}

func TestSetComponentTransform(t *testing.T) {
	d := load(t, bambuLike(t))
	warns, err := d.SetComponentTransform(d.Root.Name, 2, 1, Translation(3, 3, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(warns, "bambu-model-settings-stale") {
		t.Fatalf("warnings = %v", warns)
	}
	insts, err := load(t, save(t, d)).Instances()
	if err != nil {
		t.Fatal(err)
	}
	if got := insts[1].Point([3]float64{}); !near(got, [3]float64{103, 103, 8}) {
		t.Fatalf("got %v", got)
	}
}

func TestEditKeepsPrefixAndBOM(t *testing.T) {
	model := "\xEF\xBB\xBF" + `<?xml version="1.0" encoding="UTF-8"?>
<c:model xmlns:c="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" unit="millimeter"><c:resources><c:object id="1">` +
		cubeMesh("c", "") + `</c:object></c:resources><c:build><c:item objectid="1"/></c:build></c:model>`
	d := load(t, pkgWith(t, model))
	verts := append([][3]float64(nil), cubeVerts...)
	verts[6] = [3]float64{11, 11, 11}
	if _, err := d.SetVertices(d.Root.Name, 1, verts); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SetItemTransform(0, Translation(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	data := partBytes(t, save(t, d), "/3D/3dmodel.model")
	if !bytes.HasPrefix(data, []byte("\xEF\xBB\xBF<?xml")) {
		t.Fatal("BOM lost")
	}
	if !bytes.Contains(data, []byte(`<c:vertex x="11" y="11" z="11"/>`)) || !bytes.Contains(data, []byte(`<c:item objectid="1" transform=`)) {
		t.Fatalf("prefix not reused:\n%s", data)
	}
}

func TestReplacePart(t *testing.T) {
	d := load(t, bambuLike(t))
	if err := d.Pkg.ReplacePart("/Metadata/plate_1.png", []byte("new png")); err != nil {
		t.Fatal(err)
	}
	if err := d.Pkg.ReplacePart(d.Root.Name, nil); err == nil {
		t.Fatal("model part replacement must go through the edit API")
	}
	out := save(t, d)
	if got := partBytes(t, out, "/Metadata/plate_1.png"); string(got) != "new png" {
		t.Fatalf("got %q", got)
	}
}

func TestSaveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.3mf")
	if err := os.WriteFile(src, pkgWith(t, simpleModel("")), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out.3mf")
	if err := os.WriteFile(dst, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, err := OpenFile(src, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	d, err := Load(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SaveFile(filepath.Join(dir, "missing", "out.3mf")); err == nil {
		t.Fatal("expected error for missing directory")
	}
	pkg.Close() // raw copies now fail mid-save
	if err := d.SaveFile(dst); err == nil {
		t.Fatal("expected save failure")
	}
	if got, _ := os.ReadFile(dst); string(got) != "previous" {
		t.Fatal("failed save replaced the destination")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 2 {
		t.Fatalf("temporary file left behind: %v", ents)
	}

	pkg, err = OpenFile(src, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	if d, err = Load(pkg); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveFile(dst); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFile(dst, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

type failWriter struct{ n int }

func (w *failWriter) Write(p []byte) (int, error) {
	if w.n -= len(p); w.n < 0 {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}

func TestSaveReportsWriterErrors(t *testing.T) {
	d := load(t, bambuLike(t))
	if err := d.Save(&failWriter{n: 200}); err == nil {
		t.Fatal("writer error swallowed")
	}
}
