package threemf

import (
	"bytes"
	"encoding/xml"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func generate(t *testing.T, objs []MeshObject, opt GenerateOptions) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Generate(&buf, objs, opt); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGenerateRoundTrip(t *testing.T) {
	m := Translation(20, 0, 0)
	objs := []MeshObject{
		{Name: `cube <"&'>`, PartNumber: "P-1", Vertices: cubeVerts, Triangles: cubeTris},
		{Name: "moved", Vertices: cubeVerts, Triangles: cubeTris, Transform: &m},
	}
	b := generate(t, objs, GenerateOptions{
		Unit:            "centimeter",
		Metadata:        []Metadata{{Name: "Title", Value: "a < b & c"}, {Name: "Application", Value: "go-3mf example"}},
		ProductionUUIDs: true,
		Thumbnail:       []byte("\x89PNG"),
	})
	d := load(t, b)
	if issues := d.Validate(ProfileStrict); len(issues) != 0 {
		t.Fatalf("generated package has issues: %v", issues)
	}
	if d.Root.Unit != "centimeter" || d.Root.Object(1).Name != `cube <"&'>` || d.Root.Metadata[0].Value != "a < b & c" {
		t.Fatalf("round trip lost data: %+v", d.Root)
	}
	insts, err := d.Instances()
	if err != nil {
		t.Fatal(err)
	}
	if got := insts[1].Point([3]float64{1, 0, 0}); !near(got, [3]float64{210, 0, 0}) {
		t.Fatalf("cm scaling: got %v", got)
	}

	// An independent decode with encoding/xml structs, to not only trust our own parser.
	var model struct {
		XMLName   xml.Name `xml:"http://schemas.microsoft.com/3dmanufacturing/core/2015/02 model"`
		Required  string   `xml:"requiredextensions,attr"`
		Resources struct {
			Objects []struct {
				ID   int    `xml:"id,attr"`
				UUID string `xml:"http://schemas.microsoft.com/3dmanufacturing/production/2015/06 UUID,attr"`
				Mesh struct {
					Vertices []struct {
						X float64 `xml:"x,attr"`
					} `xml:"vertices>vertex"`
					Triangles []struct {
						V1 int `xml:"v1,attr"`
					} `xml:"triangles>triangle"`
				} `xml:"mesh"`
			} `xml:"object"`
		} `xml:"resources"`
	}
	if err := xml.Unmarshal(partBytes(t, b, "/3D/3dmodel.model"), &model); err != nil {
		t.Fatal(err)
	}
	if model.Required != "p" || len(model.Resources.Objects) != 2 || len(model.Resources.Objects[0].Mesh.Triangles) != 12 ||
		len(model.Resources.Objects[1].UUID) != 36 {
		t.Fatalf("unexpected structure: %+v", model)
	}
}

func TestGenerateRejectsInvalidInput(t *testing.T) {
	open := [][3]int{{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7}}
	inward := make([][3]int, len(cubeTris))
	for i, tri := range cubeTris {
		inward[i] = [3]int{tri[0], tri[2], tri[1]}
	}
	nan := append([][3]float64(nil), cubeVerts...)
	nan[0][0] = math.NaN()
	tests := map[string]struct {
		objs []MeshObject
		opt  GenerateOptions
	}{
		"open mesh":      {[]MeshObject{{Vertices: cubeVerts, Triangles: open}}, GenerateOptions{}},
		"inward normals": {[]MeshObject{{Vertices: cubeVerts, Triangles: inward}}, GenerateOptions{}},
		"NaN":            {[]MeshObject{{Vertices: nan, Triangles: cubeTris}}, GenerateOptions{}},
		"unit":           {[]MeshObject{{Vertices: cubeVerts, Triangles: cubeTris}}, GenerateOptions{Unit: "mm"}},
		"metadata name":  {[]MeshObject{{Vertices: cubeVerts, Triangles: cubeTris}}, GenerateOptions{Metadata: []Metadata{{Name: "Author"}}}},
		"duplicate meta": {[]MeshObject{{Vertices: cubeVerts, Triangles: cubeTris}}, GenerateOptions{Metadata: []Metadata{{Name: "Title"}, {Name: "Title"}}}},
		"no objects":     {nil, GenerateOptions{}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if err := Generate(&bytes.Buffer{}, tc.objs, tc.opt); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestGeneratedModelMatchesCoreXSD(t *testing.T) {
	xmllint, err := exec.LookPath("xmllint")
	if err != nil {
		t.Skip("xmllint not installed")
	}
	b := generate(t, []MeshObject{{Name: "cube", Vertices: cubeVerts, Triangles: cubeTris}},
		GenerateOptions{Metadata: []Metadata{{Name: "Title", Value: "x"}}, ProductionUUIDs: true})
	dir := t.TempDir()
	model := filepath.Join(dir, "3dmodel.model")
	if err := os.WriteFile(model, partBytes(t, b, "/3D/3dmodel.model"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(xmllint, "--nonet", "--noout", "--schema", "testdata/3mf-core.xsd", model).CombinedOutput()
	if err != nil {
		t.Fatalf("xmllint: %v\n%s", err, out)
	}
}

func TestProjectCompatibleFormatting(t *testing.T) {
	// Expected values were produced by Node.js v24 running the project's fmt().
	cases := map[float64]string{
		1.03125:              "1.0313",
		-0.00001:             "-0",
		-0.00006:             "-0.0001",
		0:                    "0",
		math.Copysign(0, -1): "0",
		12.5:                 "12.5",
		0.00005:              "0.0001",
		1.00005:              "1.0001",
		2.5e-5:               "0",
		123456.78915:         "123456.7891",
		-3.14159265:          "-3.1416",
		0.1 + 0.2:            "0.3",
		1e-7:                 "0",
		99999.99995:          "99999.9999",
	}
	for v, want := range cases {
		if got := FormatProjectCompatible(v); got != want {
			t.Errorf("FormatProjectCompatible(%v) = %q, want %q", v, got, want)
		}
	}
	if got := FormatNumber(1.03125); got != "1.03125" {
		t.Errorf("FormatNumber must be lossless, got %q", got)
	}
}

func TestJSRound(t *testing.T) {
	cases := map[float64]float64{0.49999999999999994: 0, -2.5: -2, 2.5: 3, -0.5: 0, 1e4 * 1.00005: 10001}
	for in, want := range cases {
		if got := jsRound(in); got != want {
			t.Errorf("jsRound(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestWeldProjectCompatible(t *testing.T) {
	// Two triangles sharing an edge; the second copy of (1,0,0) is 0.00004 off
	// and welds onto the first, keeping the first coordinates.
	soup := []float32{
		0, 0, 0, 1, 0, 0, 0, 1, 0,
		1.00004, 0, 0, 1, 1, 0, 0, 1, 0,
	}
	verts, tris := WeldProjectCompatible(soup)
	if len(verts) != 4 || tris[1][0] != 1 || verts[1][0] != 1 {
		t.Fatalf("verts %v tris %v", verts, tris)
	}
	// A sliver collapses to a repeated index, as in the project; CheckMesh flags it.
	_, tris = WeldProjectCompatible([]float32{0, 0, 0, 0.00001, 0, 0, 0, 1, 0})
	if tris[0][0] != tris[0][1] {
		t.Fatalf("expected collapse, got %v", tris)
	}
	if probs := CheckMesh([][3]float64{{}, {0, 1, 0}}, tris); !strings.Contains(strings.Join(probs, ";"), "repeats a vertex") {
		t.Fatalf("CheckMesh: %v", probs)
	}
}
