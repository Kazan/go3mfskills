package threemf

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Severity of a validation issue.
type Severity int

const (
	SeverityWarning Severity = iota
	SeverityError
)

func (s Severity) String() string {
	if s == SeverityError {
		return "error"
	}
	return "warning"
}

// Profile selects how known producer deviations are graded.
type Profile int

const (
	// ProfileStrict reports Core/Production violations as errors.
	ProfileStrict Profile = iota
	// ProfileBambu downgrades deviations Bambu Studio writes by design
	// (type="other" volumes referenced from the build, unprefixed paint_*
	// triangle attributes, /Auxiliaries/.thumbnails) to warnings.
	ProfileBambu
)

// Issue is one validation finding.
type Issue struct {
	Severity Severity
	Code     string
	Part     string
	Message  string
}

func (i Issue) String() string {
	return fmt.Sprintf("%s %s (%s): %s", i.Severity, i.Code, i.Part, i.Message)
}

// coreTriangleAttrs and coreVertexAttrs are the unprefixed attributes the
// Core schema defines; anything else unprefixed is a non-conforming extension.
var (
	coreTriangleAttrs = map[string]bool{"p1": true, "p2": true, "p3": true, "pid": true}
	coreVertexAttrs   = map[string]bool{}
)

// WellKnownMetadata are the unprefixed metadata names defined by Core 3.4.1.
var WellKnownMetadata = map[string]bool{
	"Title": true, "Designer": true, "Description": true, "Copyright": true,
	"LicenseTerms": true, "Rating": true, "CreationDate": true,
	"ModificationDate": true, "Application": true,
}

// Validate checks a loaded document beyond what Load enforces. It is a
// conformance aid, not a replacement for the 3MF Consortium's validators.
func (d *Document) Validate(profile Profile) []Issue {
	var out []Issue
	add := func(sev Severity, code, part, format string, args ...any) {
		out = append(out, Issue{sev, code, part, fmt.Sprintf(format, args...)})
	}
	deviation := SeverityError
	if profile == ProfileBambu {
		deviation = SeverityWarning
	}
	for _, w := range d.Warnings {
		add(SeverityWarning, w.Code, w.Part, "%s", w.Message)
	}

	// Package: content types, part names, relationship targets.
	for _, e := range d.Pkg.Entries() {
		if e.IsDir || strings.EqualFold(e.Name, contentTypesPart) {
			continue
		}
		if d.Pkg.ContentType(e.Name) == "" {
			add(SeverityError, "missing-content-type", e.Name, "no Default or Override content type")
		}
		segs := strings.Split(strings.TrimPrefix(e.Name, "/"), "/")
		if relsPartRE.MatchString(e.Name) {
			segs = segs[:len(segs)-1] // "/_rels/.rels" is the OPC package relationships part
		}
		for _, seg := range segs {
			if strings.HasPrefix(seg, ".") {
				add(deviation, "dot-segment", e.Name, "part name segments must not start with a period")
				break
			}
		}
	}
	for src, rels := range d.Pkg.AllRels() {
		for _, r := range rels {
			if r.External || d.Pkg.Has(r.Target) {
				continue
			}
			sev := SeverityWarning
			switch r.Type {
			case RelTypeStartPart, RelTypeThumbnail, RelTypePrintTicket:
				sev = SeverityError // Core 2.1: must be present
			}
			add(sev, "rel-target-missing", src, "relationship %s targets missing part %s", r.ID, r.Target)
		}
	}
	for _, e := range d.Pkg.Entries() {
		if e.IsDir || d.Pkg.ContentType(e.Name) != ContentTypeModel || d.Part(e.Name) != nil {
			continue
		}
		add(SeverityWarning, "model-part-unreachable", e.Name, "model part is not referenced from the root model and was not loaded")
	}

	rootRequiresP := false
	for _, uri := range d.Root.RequiredExtensions {
		if uri == NSProduction {
			rootRequiresP = true
		}
	}
	usesPath := false
	for _, it := range d.Root.Items {
		usesPath = usesPath || it.Path != ""
	}
	for _, o := range d.Root.Objects {
		for _, c := range o.Components {
			usesPath = usesPath || c.Path != ""
		}
	}
	if usesPath && !rootRequiresP {
		add(SeverityError, "production-not-required", d.Root.Name, "p:path is used but requiredextensions does not list the production namespace")
	}

	names := make([]string, 0, len(d.Parts))
	for k := range d.Parts {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		mp := d.Parts[k]
		seen := map[string]bool{}
		for _, m := range mp.Metadata {
			if seen[m.Name] {
				add(SeverityError, "duplicate-metadata", mp.Name, "metadata %q appears more than once", m.Name)
			}
			seen[m.Name] = true
			if !strings.Contains(m.Name, ":") && !WellKnownMetadata[m.Name] {
				add(SeverityWarning, "unknown-metadata", mp.Name, "unprefixed metadata %q is not a Core well-known name", m.Name)
			}
		}
		if mp != d.Root && len(mp.Metadata) > 0 {
			add(SeverityWarning, "non-root-metadata", mp.Name, "model metadata is only meaningful in the root model part")
		}
		if rootRequiresP {
			if mp == d.Root && mp.BuildUUID == "" {
				add(SeverityError, "missing-uuid", mp.Name, "<build> lacks p:UUID")
			}
			if mp == d.Root {
				for i, it := range mp.Items {
					if it.UUID == "" {
						add(SeverityError, "missing-uuid", mp.Name, "item %d lacks p:UUID", i)
					}
				}
			}
		}
		for _, o := range mp.Objects {
			if rootRequiresP && o.UUID == "" {
				add(SeverityError, "missing-uuid", mp.Name, "object %d lacks p:UUID", o.ID)
			}
			if o.Components != nil && o.PID != "" {
				add(SeverityError, "components-with-pid", mp.Name, "object %d has components and a pid", o.ID)
			}
			for ci, c := range o.Components {
				if rootRequiresP && c.UUID == "" {
					add(SeverityError, "missing-uuid", mp.Name, "object %d component %d lacks p:UUID", o.ID, ci)
				}
				if c.Path == "" {
					ref := mp.Object(c.ObjectID)
					if ref == nil {
						add(SeverityError, "missing-object", mp.Name, "object %d component %d references unknown object %d", o.ID, ci, c.ObjectID)
					} else if ref.Order >= o.Order {
						add(SeverityError, "forward-reference", mp.Name, "object %d references object %d defined later", o.ID, c.ObjectID)
					}
				}
			}
			if o.Mesh != nil {
				out = append(out, meshIssues(mp.Name, o, deviation)...)
			}
		}
	}

	// Build: references, and type="other" reached from an item.
	insts, err := d.Instances()
	if err != nil {
		add(SeverityError, "build-unresolvable", d.Root.Name, "%v", err)
		return out
	}
	for i, it := range d.Root.Items {
		part := d.Root.Name
		if it.Path != "" {
			part = it.Path
		}
		if mp := d.Part(part); mp != nil {
			if o := mp.Object(it.ObjectID); o != nil && o.Type == "other" {
				add(SeverityError, "item-references-other", d.Root.Name, "item %d references type=other object %d", i, o.ID)
			}
		}
	}
	other := map[string]bool{}
	for _, in := range insts {
		if in.Object.Type == "other" {
			other[fmt.Sprintf("%s#%d", in.Part, in.Object.ID)] = true
		}
	}
	for _, k := range sortedKeys(other) {
		add(deviation, "build-includes-other", k, "type=other object is reached from the build through components (Bambu modifier/negative volume?)")
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func meshIssues(part string, o *Object, deviation Severity) []Issue {
	var out []Issue
	for _, k := range sortedKeysInt(o.Mesh.TriangleAttrs) {
		if !strings.HasPrefix(k, "{") && !coreTriangleAttrs[k] {
			out = append(out, Issue{deviation, "unprefixed-extension-attribute", part, fmt.Sprintf("object %d: triangle attribute %q has no namespace prefix", o.ID, k)})
		}
	}
	for _, k := range sortedKeysInt(o.Mesh.VertexAttrs) {
		if !strings.HasPrefix(k, "{") && !coreVertexAttrs[k] {
			out = append(out, Issue{deviation, "unprefixed-extension-attribute", part, fmt.Sprintf("object %d: vertex attribute %q has no namespace prefix", o.ID, k)})
		}
	}
	if o.Type != "model" && o.Type != "solidsupport" {
		return out // only these types must be closed, oriented solids
	}
	for _, p := range CheckMesh(o.Mesh.Vertices, o.Mesh.Triangles) {
		out = append(out, Issue{SeverityError, "mesh", part, fmt.Sprintf("object %d: %s", o.ID, p)})
	}
	return out
}

func sortedKeysInt(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CheckMesh tests the Core 4.1 requirements for a model/solidsupport mesh:
// at least 4 triangles, three distinct in-range indices per triangle, every
// edge shared by exactly two triangles with opposite direction (manifold and
// consistently oriented), and positive signed volume (outward normals).
// It returns human-readable problems; nil means the mesh passed.
func CheckMesh(verts [][3]float64, tris [][3]int) []string {
	var probs []string
	const maxListed = 5
	more := map[string]int{}
	report := func(kind, msg string) {
		if more[kind] < maxListed {
			probs = append(probs, msg)
		}
		more[kind]++
	}
	if len(tris) < 4 {
		report("count", fmt.Sprintf("%d triangles; a closed mesh needs at least 4", len(tris)))
	}
	type edge struct{ a, b int }
	edges := make(map[edge]int, len(tris)*3)
	vol := 0.0
	for i, t := range tris {
		bad := false
		for _, v := range t {
			if v < 0 || v >= len(verts) {
				report("index", fmt.Sprintf("triangle %d: vertex index %d out of range", i, v))
				bad = true
			}
		}
		if bad {
			continue
		}
		if t[0] == t[1] || t[1] == t[2] || t[0] == t[2] {
			report("degenerate", fmt.Sprintf("triangle %d repeats a vertex", i))
			continue
		}
		for j := 0; j < 3; j++ {
			edges[edge{t[j], t[(j+1)%3]}]++
		}
		a, b, c := verts[t[0]], verts[t[1]], verts[t[2]]
		vol += a[0]*(b[1]*c[2]-b[2]*c[1]) - a[1]*(b[0]*c[2]-b[2]*c[0]) + a[2]*(b[0]*c[1]-b[1]*c[0])
	}
	for e, n := range edges {
		switch {
		case n > 1:
			report("orientation", fmt.Sprintf("edge %d->%d used %d times in the same direction (inconsistent winding or non-manifold)", e.a, e.b, n))
		case edges[edge{e.b, e.a}] != 1:
			report("open", fmt.Sprintf("edge %d->%d has no single opposite edge (open or non-manifold)", e.a, e.b))
		}
	}
	if len(probs) == 0 && !(vol > 0) {
		probs = append(probs, fmt.Sprintf("signed volume %g is not positive; normals point inward", vol/6))
	}
	for _, kind := range []string{"index", "degenerate", "orientation", "open"} {
		if n := more[kind]; n > maxListed {
			probs = append(probs, fmt.Sprintf("... and %d more %s problems", n-maxListed, kind))
		}
	}
	if math.IsNaN(vol) {
		probs = append(probs, "non-finite coordinates")
	}
	return probs
}
