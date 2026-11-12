package threemf

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

const bambuNS = `xmlns:BambuStudio="http://schemas.bambulab.com/package/2021" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06"`

// bambuLike mimics the Bambu Studio layout observed in bbs_3mf.cpp: a root
// components object pointing into 3D/Objects/object_1.model, unprefixed
// paint_color triangle attributes, a type="other" modifier volume, and
// derived Metadata/thumbnail parts. It is synthetic, not a real export.
func bambuLike(t testing.TB) []byte {
	root := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" ` + bambuNS + ` requiredextensions="p">
 <metadata name="Application">BambuStudio-01.10.00.00</metadata>
 <metadata name="BambuStudio:3mfVersion">1</metadata>
 <resources>
  <object id="2" p:UUID="00000001-61cb-4c03-9d28-80fed5dfa1dc" type="model">
   <components>
    <component p:path="/3D/Objects/object_1.model" objectid="1" p:UUID="00010000-b206-40ff-9872-83e8017abed1" transform="1 0 0 0 1 0 0 0 1 0 0 0"/>
    <component p:path="/3D/Objects/object_1.model" objectid="3" p:UUID="00010001-b206-40ff-9872-83e8017abed1" transform="1 0 0 0 1 0 0 0 1 2 2 2"/>
   </components>
  </object>
 </resources>
 <build p:UUID="2c7c17d8-22b5-4d84-8835-1976022ea369">
  <item objectid="2" p:UUID="00000002-b1ec-4553-aec9-835e5b724bb4" transform="1 0 0 0 1 0 0 0 1 100 100 5" printable="1"/>
 </build>
</model>`
	sub := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" ` + bambuNS + ` requiredextensions="p">
 <metadata name="BambuStudio:3mfVersion">1</metadata>
 <resources>
  <object id="1" p:UUID="00010000-81cb-4c03-9d28-80fed5dfa1dc" type="model">` + cubeMesh("", ` paint_color="4"`) + `</object>
  <object id="3" p:UUID="00010001-81cb-4c03-9d28-80fed5dfa1dc" type="other">` + cubeMesh("", "") + `</object>
 </resources>
 <build/>
</model>`
	settings := `<?xml version="1.0" encoding="UTF-8"?>
<config><object id="2"><metadata key="name" value="cube"/><part id="1" subtype="normal_part"><mesh_stat face_count="12"/></part></object></config>`
	return zipOf(t,
		file{name: "[Content_Types].xml", data: contentTypes},
		file{name: "_rels/.rels", data: rels(
			"/3D/3dmodel.model", RelTypeStartPart,
			"/Metadata/plate_1.png", RelTypeThumbnail,
			"/Auxiliaries/.thumbnails/thumbnail_small.png", "http://schemas.bambulab.com/package/2021/cover-thumbnail-small")},
		file{name: "3D/3dmodel.model", data: root},
		file{name: "3D/_rels/3dmodel.model.rels", data: rels("/3D/Objects/object_1.model", RelTypeStartPart)},
		file{name: "3D/Objects/object_1.model", data: sub},
		file{name: "Metadata/model_settings.config", data: settings},
		file{name: "Metadata/project_settings.config", data: `{"layer_height":"0.2"}`},
		file{name: "Metadata/plate_1.png", data: "\x89PNG fake", stored: true},
		file{name: "Auxiliaries/.thumbnails/thumbnail_small.png", data: "\x89PNG small", stored: true},
	)
}

func TestLoadUsesStartPartNotFileName(t *testing.T) {
	decoy := strings.Replace(simpleModel(""), `x="10"`, `x="99"`, 1)
	b := zipOf(t,
		file{name: "[Content_Types].xml", data: contentTypes},
		startRels("/models/main.model"),
		file{name: "3D/3dmodel.model", data: decoy},
		file{name: "models/main.model", data: simpleModel("")},
	)
	d := load(t, b)
	if d.Root.Name != "/models/main.model" {
		t.Fatalf("root = %s", d.Root.Name)
	}
	if d.Part("/3D/3dmodel.model") != nil {
		t.Fatal("decoy model part was loaded")
	}
	if issues := d.Validate(ProfileStrict); !hasIssue(issues, "model-part-unreachable") {
		t.Fatalf("unreachable model part not reported: %v", issues)
	}
}

func TestStartPartErrors(t *testing.T) {
	model := file{name: "3D/3dmodel.model", data: simpleModel("")}
	ct := file{name: "[Content_Types].xml", data: contentTypes}
	tests := map[string]struct {
		files []file
		want  error
	}{
		"missing": {[]file{ct, {name: "_rels/.rels", data: rels()}, model}, ErrInvalidPackage},
		"duplicate": {[]file{ct, {name: "_rels/.rels", data: rels(
			"/3D/3dmodel.model", RelTypeStartPart, "/3D/3dmodel.model", RelTypeStartPart)}, model}, ErrInvalidPackage},
		"target absent":    {[]file{ct, startRels("/3D/missing.model"), model}, ErrPartNotFound},
		"no content types": {[]file{startRels("/3D/3dmodel.model"), model}, ErrInvalidPackage},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := loadErr(zipOf(t, tc.files...), DefaultLimits())
			mustErrIs(t, err, tc.want)
		})
	}
}

func TestBambuLikeMultiPart(t *testing.T) {
	d := load(t, bambuLike(t))
	if len(d.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", d.Warnings)
	}
	sub := d.Part("/3D/Objects/object_1.model")
	if sub == nil || sub.Object(1) == nil || d.Root.Object(1) != nil {
		t.Fatal("object ids must be scoped per model part")
	}
	if got := sub.Object(1).Mesh.TriangleAttrs["paint_color"]; got != 12 {
		t.Fatalf("paint_color count = %d", got)
	}
	insts, err := d.Instances()
	if err != nil {
		t.Fatal(err)
	}
	if len(insts) != 2 {
		t.Fatalf("instances = %d", len(insts))
	}
	if got := insts[1].Point([3]float64{0, 0, 0}); !near(got, [3]float64{102, 102, 7}) {
		t.Fatalf("modifier origin = %v", got)
	}
	tris, warns, err := d.Flatten(FlattenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tris) != 12 || !hasWarning(warns, "object-type-skipped") {
		t.Fatalf("flatten: %d triangles, warnings %v", len(tris), warns)
	}

	strict, bambu := d.Validate(ProfileStrict), d.Validate(ProfileBambu)
	for _, code := range []string{"build-includes-other", "unprefixed-extension-attribute", "dot-segment"} {
		if sev, ok := issueSeverity(strict, code); !ok || sev != SeverityError {
			t.Errorf("strict %s: got %v %v", code, sev, ok)
		}
		if sev, ok := issueSeverity(bambu, code); !ok || sev != SeverityWarning {
			t.Errorf("bambu %s: got %v %v", code, sev, ok)
		}
	}
	for _, is := range bambu {
		if is.Severity == SeverityError {
			t.Errorf("bambu profile error: %v", is)
		}
	}
}

func TestComponentTransformOrder(t *testing.T) {
	// obj3 = rotate90z(obj2), obj2 = translate(10,0,0)(obj1), item = translate(0,0,5).
	// Applying children first moves (0,0,0) to (0,10,5); the reverse order gives (10,0,5).
	model := modelOpen + `<resources>
<object id="1">` + cubeMesh("", "") + `</object>
<object id="2"><components><component objectid="1" transform="1 0 0 0 1 0 0 0 1 10 0 0"/></components></object>
<object id="3"><components><component objectid="2" transform="0 1 0 -1 0 0 0 0 1 0 0 0"/></components></object>
</resources><build><item objectid="3" transform="1 0 0 0 1 0 0 0 1 0 0 5"/></build></model>`
	d := load(t, pkgWith(t, model))
	insts, err := d.Instances()
	if err != nil {
		t.Fatal(err)
	}
	if got := insts[0].Point([3]float64{0, 0, 0}); !near(got, [3]float64{0, 10, 5}) {
		t.Fatalf("origin maps to %v, want [0 10 5]", got)
	}
}

func TestUnitsAndMixedUnits(t *testing.T) {
	root := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06" requiredextensions="p">
<resources><object id="1" p:UUID="a"><components><component objectid="1" p:path="/3D/inch.model" p:UUID="b"/></components></object></resources>
<build p:UUID="c"><item objectid="1" p:UUID="d" transform="1 0 0 0 1 0 0 0 1 1 0 0"/></build></model>`
	sub := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="inch" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06">
<resources><object id="1" p:UUID="e">` + cubeMesh("", "") + `</object></resources><build/></model>`
	d := load(t, zipOf(t,
		file{name: "[Content_Types].xml", data: contentTypes},
		startRels("/3D/root.model"),
		file{name: "3D/root.model", data: root},
		file{name: "3D/_rels/root.model.rels", data: rels("inch.model", RelTypeStartPart)}, // relative target
		file{name: "3D/inch.model", data: sub},
	))
	if !hasWarning(d.Warnings, "mixed-units") {
		t.Fatalf("warnings = %v", d.Warnings)
	}
	insts, err := d.Instances()
	if err != nil {
		t.Fatal(err)
	}
	// Vertex (10,0,0) inch = 254 mm, plus the root item's 1 mm translation.
	if got := insts[0].Point([3]float64{10, 0, 0}); !near(got, [3]float64{255, 0, 0}) {
		t.Fatalf("got %v", got)
	}
}

func TestMirroredItemKeepsOutwardWinding(t *testing.T) {
	d := load(t, pkgWith(t, simpleModel(` transform="-1 0 0 0 1 0 0 0 1 0 0 0"`)))
	tris, _, err := d.Flatten(FlattenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	vol := 0.0
	for _, t := range tris {
		a, b, c := t[0], t[1], t[2]
		vol += a[0]*(b[1]*c[2]-b[2]*c[1]) - a[1]*(b[0]*c[2]-b[2]*c[0]) + a[2]*(b[0]*c[1]-b[1]*c[0])
	}
	if vol <= 0 {
		t.Fatalf("signed volume %g after mirroring; winding not flipped", vol/6)
	}
}

func TestUnreferencedObjectsAreNotEmitted(t *testing.T) {
	model := modelOpen + `<resources><object id="1">` + cubeMesh("", "") + `</object><object id="2">` +
		cubeMesh("", "") + `</object></resources><build><item objectid="2"/></build></model>`
	tris, _, err := load(t, pkgWith(t, model)).Flatten(FlattenOptions{})
	if err != nil || len(tris) != 12 {
		t.Fatalf("got %d triangles, err %v", len(tris), err)
	}
	empty := modelOpen + `<resources><object id="1">` + cubeMesh("", "") + `</object></resources><build/></model>`
	tris, _, err = load(t, pkgWith(t, empty)).Flatten(FlattenOptions{})
	if err != nil || len(tris) != 0 {
		t.Fatalf("empty build produced %d triangles (err %v); consumers must not invent items", len(tris), err)
	}
}

func TestMalformedInput(t *testing.T) {
	mesh := func(v, tri string) string {
		return modelOpen + `<resources><object id="1"><mesh><vertices>` + v + `</vertices><triangles>` + tri +
			`</triangles></mesh></object></resources><build><item objectid="1"/></build></model>`
	}
	okV := `<vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/>`
	okT := `<triangle v1="0" v2="1" v3="2"/>`
	tests := map[string]struct {
		model string
		want  error
	}{
		"comma decimal":   {mesh(strings.Replace(okV, `x="1"`, `x="1,5"`, 1), okT), ErrMalformedModel},
		"NaN":             {mesh(strings.Replace(okV, `x="1"`, `x="NaN"`, 1), okT), ErrMalformedModel},
		"trailing dot":    {mesh(strings.Replace(okV, `x="1"`, `x="1."`, 1), okT), ErrMalformedModel},
		"overflow":        {mesh(strings.Replace(okV, `x="1"`, `x="1e999"`, 1), okT), ErrMalformedModel},
		"negative index":  {mesh(okV, `<triangle v1="-1" v2="1" v3="2"/>`), ErrMalformedModel},
		"index too large": {mesh(okV, `<triangle v1="0" v2="1" v3="3"/>`), ErrMalformedModel},
		"missing v3":      {mesh(okV, `<triangle v1="0" v2="1"/>`), ErrMalformedModel},
		"doctype": {`<?xml version="1.0"?><!DOCTYPE model [<!ENTITY x "y">]>` +
			strings.TrimPrefix(simpleModel(""), `<?xml version="1.0" encoding="UTF-8"?>`), ErrMalformedModel},
		"wrong root ns": {`<model xmlns="urn:other"/>`, ErrMalformedModel},
		"bad transform": {simpleModel(` transform="1 0 0"`), ErrMalformedModel},
		"bad unit":      {strings.Replace(simpleModel(""), "millimeter", "Millimeter", 1), ErrMalformedModel},
		"duplicate id": {modelOpen + `<resources><object id="1">` + cubeMesh("", "") + `</object><basematerials id="1"/></resources><build/></model>`,
			ErrMalformedModel},
		"zero id":           {strings.Replace(simpleModel(""), `id="1"`, `id="0"`, 1), ErrMalformedModel},
		"undeclared prefix": {strings.Replace(simpleModel(""), `xml:lang="en-US"`, `requiredextensions="q"`, 1), ErrMalformedModel},
		"unsupported required": {strings.Replace(simpleModel(""), `xml:lang="en-US"`,
			`xmlns:s="http://schemas.microsoft.com/3dmanufacturing/slice/2015/07" requiredextensions="s"`, 1), ErrUnsupportedRequire},
		"relative p:path": {strings.Replace(simpleModel(""), `<item objectid="1"`,
			`<item xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06" p:path="3D/x.model" objectid="1"`, 1), ErrMalformedModel},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := loadErr(pkgWith(t, tc.model), DefaultLimits())
			mustErrIs(t, err, tc.want)
		})
	}
}

func TestInvalidReferences(t *testing.T) {
	cycle := modelOpen + `<resources>
<object id="1"><components><component objectid="2"/></components></object>
<object id="2"><components><component objectid="1"/></components></object>
</resources><build><item objectid="1"/></build></model>`
	d := load(t, pkgWith(t, cycle))
	_, err := d.Instances()
	mustErrIs(t, err, ErrInvalidReference)
	if !hasIssue(d.Validate(ProfileStrict), "forward-reference") {
		t.Fatal("forward reference not reported")
	}

	missing := modelOpen + `<resources/><build><item objectid="7"/></build></model>`
	_, err = load(t, pkgWith(t, missing)).Instances()
	mustErrIs(t, err, ErrInvalidReference)

	nonRootPath := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06">
<resources><object id="1"><components><component objectid="1" p:path="/3D/c.model"/></components></object></resources></model>`
	root := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06" requiredextensions="p">
<resources><object id="1"><components><component objectid="1" p:path="/3D/b.model"/></components></object></resources>
<build><item objectid="1"/></build></model>`
	_, err = loadErr(zipOf(t,
		file{name: "[Content_Types].xml", data: contentTypes},
		startRels("/3D/3dmodel.model"),
		file{name: "3D/3dmodel.model", data: root},
		file{name: "3D/b.model", data: nonRootPath},
		file{name: "3D/c.model", data: simpleModel("")},
	), DefaultLimits())
	mustErrIs(t, err, ErrInvalidReference)
}

func TestModelPartNotInRelsIsLoadedWithWarning(t *testing.T) {
	root := `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06" requiredextensions="p">
<resources><object id="1"><components><component objectid="1" p:path="/3D/b.model"/></components></object></resources>
<build><item objectid="1"/></build></model>`
	d := load(t, zipOf(t,
		file{name: "[Content_Types].xml", data: contentTypes},
		startRels("/3D/3dmodel.model"),
		file{name: "3D/3dmodel.model", data: root},
		file{name: "3D/b.model", data: simpleModel("")},
	))
	if !hasWarning(d.Warnings, "model-part-not-in-rels") {
		t.Fatalf("warnings = %v", d.Warnings)
	}
}

func TestPackageLevelRejections(t *testing.T) {
	ct := file{name: "[Content_Types].xml", data: contentTypes}
	tests := map[string][]file{
		"case-insensitive duplicate": {ct, startRels("/3D/3dmodel.model"),
			{name: "3D/3dmodel.model", data: simpleModel("")}, {name: "3D/3DModel.model", data: simpleModel("")}},
		"zip slip":      {ct, startRels("/3D/3dmodel.model"), {name: "../evil.model", data: "x"}},
		"backslash":     {ct, startRels("/3D/3dmodel.model"), {name: `3D\x.model`, data: "x"}},
		"rels with DTD": {ct, {name: "_rels/.rels", data: `<?xml version="1.0"?><!DOCTYPE r [<!ENTITY a "b">]><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`}},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := OpenBytes(zipOf(t, files...), DefaultLimits())
			mustErrIs(t, err, ErrInvalidPackage)
		})
	}
}

func TestCorruptedCRC(t *testing.T) {
	model := simpleModel("")
	b := zipOf(t,
		file{name: "[Content_Types].xml", data: contentTypes},
		startRels("/3D/3dmodel.model"),
		file{name: "3D/3dmodel.model", data: model, stored: true},
	)
	i := bytes.Index(b, []byte(`x="10"`))
	b[i+3] = '9' // same length, different bytes: only the CRC can catch it
	_, err := loadErr(b, DefaultLimits())
	if !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("err = %v, want zip.ErrChecksum", err)
	}
}

func TestLimits(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxTriangles = 11
	_, err := loadErr(pkgWith(t, simpleModel("")), lim)
	mustErrIs(t, err, ErrLimitExceeded)

	lim = DefaultLimits()
	lim.MaxPartSize = 100
	_, err = loadErr(pkgWith(t, simpleModel("")), lim)
	mustErrIs(t, err, ErrLimitExceeded)

	lim = DefaultLimits()
	lim.MaxEntries = 2
	_, err = loadErr(pkgWith(t, simpleModel("")), lim)
	mustErrIs(t, err, ErrLimitExceeded)

	lim = DefaultLimits()
	lim.MaxComponentDepth = 2
	deep := modelOpen + `<resources><object id="1">` + cubeMesh("", "") + `</object>
<object id="2"><components><component objectid="1"/></components></object>
<object id="3"><components><component objectid="2"/></components></object>
</resources><build><item objectid="3"/></build></model>`
	d, err := loadErr(pkgWith(t, deep), lim)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Instances()
	mustErrIs(t, err, ErrLimitExceeded)

	// Instancing multiplies triangles: 12 per copy, 2 copies, limit 20.
	lim = DefaultLimits()
	lim.MaxTriangles = 20
	twice := modelOpen + `<resources><object id="1">` + cubeMesh("", "") +
		`</object></resources><build><item objectid="1"/><item objectid="1"/></build></model>`
	d, err = loadErr(pkgWith(t, twice), lim)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = d.Flatten(FlattenOptions{})
	mustErrIs(t, err, ErrLimitExceeded)
}

func TestMatrixParsingAndComposition(t *testing.T) {
	m, err := ParseMatrix(" 2 0 0\n0 2 0 0 0 2 1 2 3 ")
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Apply([3]float64{1, 1, 1}); !near(got, [3]float64{3, 4, 5}) {
		t.Fatalf("apply = %v", got)
	}
	back, err := ParseMatrix(m.String())
	if err != nil || back != m {
		t.Fatalf("round trip %v %v", back, err)
	}
	if Translation(1, 0, 0).Then(Matrix{-1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}).Apply([3]float64{}) != [3]float64{-1, 0, 0} {
		t.Fatal("Then must apply the receiver first")
	}
}

func hasIssue(is []Issue, code string) bool {
	_, ok := issueSeverity(is, code)
	return ok
}

func issueSeverity(is []Issue, code string) (Severity, bool) {
	for _, i := range is {
		if i.Code == code {
			return i.Severity, true
		}
	}
	return 0, false
}
