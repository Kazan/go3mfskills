package threemf

import (
	"fmt"
	"strings"
)

// Warning reports a condition the caller should surface to the user. Warnings
// never mean data was silently dropped: they name what may now be stale,
// skipped or unsupported.
type Warning struct {
	Code    string
	Part    string
	Message string
}

func (w Warning) String() string {
	if w.Part == "" {
		return w.Code + ": " + w.Message
	}
	return w.Code + " (" + w.Part + "): " + w.Message
}

// SupportedRequiredExtensions are the namespaces this example can process
// when a model lists them in requiredextensions.
var SupportedRequiredExtensions = map[string]bool{
	NSProduction:   true,
	NSTriangleSets: true, // informational only; triangle sets do not affect shape
}

// Document is the package plus every reachable model part.
type Document struct {
	Pkg      *Package
	Root     *ModelPart
	Parts    map[string]*ModelPart // lower-cased part name -> part
	Warnings []Warning

	splices map[string][]splice // lower-cased part name -> pending edits
}

// Load discovers the root model through the package StartPart relationship,
// parses it and every model part it references, and checks required
// extensions. It never guesses the root from file names.
func Load(pkg *Package) (*Document, error) {
	starts := pkg.RelsOfType("/", RelTypeStartPart)
	switch {
	case len(starts) == 0:
		return nil, fmt.Errorf("%w: no StartPart relationship in /_rels/.rels", ErrInvalidPackage)
	case len(starts) > 1:
		return nil, fmt.Errorf("%w: %d StartPart relationships in /_rels/.rels", ErrInvalidPackage, len(starts))
	case starts[0].External:
		return nil, fmt.Errorf("%w: StartPart target is external", ErrInvalidPackage)
	}
	d := &Document{Pkg: pkg, Parts: map[string]*ModelPart{}, splices: map[string][]splice{}}
	budget := pkg.limits.MaxTriangles
	root, err := d.loadPart(starts[0].Target, &budget)
	if err != nil {
		return nil, err
	}
	d.Root = root

	// Production 3.3: non-root model parts are referenced from the root
	// model part's .rels with the 3D model relationship type.
	for _, r := range pkg.RelsOfType(root.Name, RelTypeStartPart) {
		if r.External {
			return nil, fmt.Errorf("%w: external model reference %q", ErrInvalidPackage, r.RawTarget)
		}
		if _, err := d.loadPart(r.Target, &budget); err != nil {
			return nil, err
		}
	}
	// Paths that the relationships do not declare are still followed, but
	// reported, because some producers omit the model .rels.
	var refs []string
	for _, it := range root.Items {
		refs = append(refs, it.Path)
	}
	for _, o := range root.Objects {
		for _, c := range o.Components {
			refs = append(refs, c.Path)
		}
	}
	for _, ref := range refs {
		if ref == "" || d.Parts[strings.ToLower(ref)] != nil {
			continue
		}
		if _, err := d.loadPart(ref, &budget); err != nil {
			return nil, fmt.Errorf("%w: p:path %q: %w", ErrInvalidReference, ref, err)
		}
		d.warn("model-part-not-in-rels", ref, "model part is referenced by p:path but not by the root model's relationships")
	}

	for _, mp := range d.Parts {
		for _, uri := range mp.RequiredExtensions {
			if !SupportedRequiredExtensions[uri] {
				return nil, fmt.Errorf("%w: %s requires %s", ErrUnsupportedRequire, mp.Name, uri)
			}
		}
		if mp == root {
			continue
		}
		if len(mp.Items) > 0 {
			d.warn("non-root-build-ignored", mp.Name, "build items in non-root model parts are ignored (Production ch. 2)")
		}
		for _, o := range mp.Objects {
			for _, c := range o.Components {
				if c.Path != "" {
					return nil, fmt.Errorf("%w: %s object %d: p:path is only allowed in the root model part", ErrInvalidReference, mp.Name, o.ID)
				}
			}
		}
		if mp.Unit != root.Unit {
			d.warn("mixed-units", mp.Name, fmt.Sprintf("part unit %q differs from root unit %q; vertices use the part unit, transforms the unit of the part that contains them", mp.Unit, root.Unit))
		}
	}
	return d, nil
}

func (d *Document) warn(code, part, msg string) {
	d.Warnings = append(d.Warnings, Warning{Code: code, Part: part, Message: msg})
}

func (d *Document) loadPart(name string, budget *int) (*ModelPart, error) {
	if mp := d.Parts[strings.ToLower(name)]; mp != nil {
		return mp, nil
	}
	data, err := d.Pkg.Read(name)
	if err != nil {
		return nil, err // missing StartPart target must be an error (Core 2.1.1)
	}
	if ct := d.Pkg.ContentType(name); ct != ContentTypeModel {
		d.warn("model-content-type", name, fmt.Sprintf("content type is %q, want %q", ct, ContentTypeModel))
	}
	mp, err := ParseModel(name, data, budget)
	if err != nil {
		return nil, err
	}
	d.Parts[strings.ToLower(name)] = mp
	return mp, nil
}

// Part returns a model part by name (case-insensitive), or nil.
func (d *Document) Part(name string) *ModelPart { return d.Parts[strings.ToLower(name)] }

// Instance is one mesh object placed in the build.
type Instance struct {
	Item      int    // index into Root.Items
	Part      string // part that defines the mesh object
	Object    *Object
	UnitScale float64 // scale applied to the object's vertices before World
	World     Matrix  // millimetre transform, after UnitScale
	Chain     []string
}

// Point maps a local vertex of the instance's mesh to world millimetres.
func (in Instance) Point(v [3]float64) [3]float64 {
	s := in.UnitScale
	return in.World.Apply([3]float64{v[0] * s, v[1] * s, v[2] * s})
}

// Mirrored reports whether the instance transform has a negative determinant.
func (in Instance) Mirrored() bool { return in.World.Determinant() < 0 }

// Instances expands build items and components into placed mesh objects.
// It rejects missing objects, cycles and excessive depth. Objects that are
// not referenced from the build are never emitted (Core 3.4.3).
func (d *Document) Instances() ([]Instance, error) {
	var out []Instance
	for i, it := range d.Root.Items {
		part := d.Root.Name
		if it.Path != "" {
			part = it.Path
		}
		m := it.Transform.scaleTranslation(d.Root.UnitScale)
		chain := []string{fmt.Sprintf("item[%d]", i)}
		if err := d.walk(i, part, it.ObjectID, m, map[string]bool{}, chain, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (d *Document) walk(item int, part string, id int, world Matrix, visiting map[string]bool, chain []string, out *[]Instance) error {
	if len(chain) > d.Pkg.limits.MaxComponentDepth {
		return fmt.Errorf("%w: component depth exceeds %d at %s", ErrLimitExceeded, d.Pkg.limits.MaxComponentDepth, strings.Join(chain, " > "))
	}
	mp := d.Part(part)
	if mp == nil {
		return fmt.Errorf("%w: model part %s is not loaded", ErrInvalidReference, part)
	}
	obj := mp.Object(id)
	if obj == nil {
		return fmt.Errorf("%w: %s has no object %d (%s)", ErrInvalidReference, part, id, strings.Join(chain, " > "))
	}
	key := strings.ToLower(part) + "#" + fmt.Sprint(id)
	if visiting[key] {
		return fmt.Errorf("%w: component cycle at %s", ErrInvalidReference, key)
	}
	visiting[key] = true
	defer delete(visiting, key)
	chain = append(chain, key)

	if obj.Mesh != nil {
		*out = append(*out, Instance{
			Item: item, Part: mp.Name, Object: obj, UnitScale: mp.UnitScale,
			World: world, Chain: append([]string(nil), chain...),
		})
		return nil
	}
	for _, c := range obj.Components {
		cpart := mp.Name
		if c.Path != "" {
			cpart = c.Path
		}
		// The component's translation is in the unit of the part holding it.
		child := c.Transform.scaleTranslation(mp.UnitScale).Then(world)
		if err := d.walk(item, cpart, c.ObjectID, child, visiting, chain, out); err != nil {
			return err
		}
	}
	return nil
}

// FlattenOptions selects which object types become geometry.
type FlattenOptions struct {
	// Types to include. Empty means "model" and "solidsupport", the types
	// that must be closed solids. Bambu Studio stores modifier and negative
	// volumes as type "other" components; include them only deliberately.
	Types []string
}

// Flatten returns world-space (mm) triangles for the build. This is a lossy,
// derived view: object identity, materials, paint and metadata are gone.
// Never use it as the source for saving a package-preserving edit.
func (d *Document) Flatten(opt FlattenOptions) ([][3][3]float64, []Warning, error) {
	types := opt.Types
	if len(types) == 0 {
		types = []string{"model", "solidsupport"}
	}
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	insts, err := d.Instances()
	if err != nil {
		return nil, nil, err
	}
	var warns []Warning
	skipped := map[string]int{}
	total := 0
	for _, in := range insts {
		if want[in.Object.Type] {
			total += len(in.Object.Mesh.Triangles)
		}
	}
	if total > d.Pkg.limits.MaxTriangles {
		return nil, nil, fmt.Errorf("%w: build expands to %d triangles", ErrLimitExceeded, total)
	}
	out := make([][3][3]float64, 0, total)
	for _, in := range insts {
		if !want[in.Object.Type] {
			skipped[in.Object.Type]++
			continue
		}
		mirror := in.Mirrored()
		vs := in.Object.Mesh.Vertices
		for _, t := range in.Object.Mesh.Triangles {
			a, b, c := in.Point(vs[t[0]]), in.Point(vs[t[1]]), in.Point(vs[t[2]])
			if mirror {
				b, c = c, b // keep outward orientation (Core 3.3)
			}
			out = append(out, [3][3]float64{a, b, c})
		}
	}
	for typ, n := range skipped {
		warns = append(warns, Warning{Code: "object-type-skipped", Message: fmt.Sprintf("%d instance(s) of type %q not included", n, typ)})
	}
	return out, warns, nil
}

// References returns how many placed instances use the object, which tells
// whether editing the shared resource changes more than one copy.
func (d *Document) References(part string, id int) (int, error) {
	insts, err := d.Instances()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, in := range insts {
		if strings.EqualFold(in.Part, part) && in.Object.ID == id {
			n++
		}
	}
	return n, nil
}
