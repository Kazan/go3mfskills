# Generating 3MF in Go

Generation is the simple direction. You control every byte, so write the
minimum that is conformant and validate it before writing.
([generate.go](../scripts/threemf/generate.go))

## Workflow

1. **Weld** the triangle soup into indexed vertices. Core requires a
   closed, manifold mesh with every edge shared by exactly two triangles,
   and a soup cannot satisfy that.
   - Quantize to a grid that suits your unit. stlTexturizer uses 1e-4 mm.
   - After welding, **drop triangles that collapsed** (repeated index).
     stlTexturizer does not do this. This is a Project gap; see [project-analysis.md](project-analysis.md).
2. **Check** with `CheckMesh`, which requires all of the following:
   - at least 4 triangles;
   - in-range indices with no repeats;
   - each directed edge used once, with its reverse also used once;
   - signed volume > 0.

   `Generate` runs it unless `SkipMeshCheck` is set. Don't set it for
   `type="model"`.
3. **Describe** the objects: `[]MeshObject{Name, Vertices, Triangles,
   Transform}`. Choose `GenerateOptions`:
   - `Unit` (default `millimeter`);
   - `Metadata`: well-known names, or names with the `p:` prefix;
   - `ProductionUUIDs`: Bambu writes them, and they are harmless elsewhere;
   - `Thumbnail` (PNG).
4. **Write** with `Generate(w, objs, opt)`. Its output, in order:
   - `[Content_Types].xml` with Defaults for rels, model and png;
   - `_rels/.rels` with a StartPart rel to `/3D/3dmodel.model` (absolute
     target) and an optional Thumbnail rel;
   - the model, streamed through `bufio`, with numbers written by
     `strconv.FormatFloat(v,'f',-1,64)`. That form round-trips exactly and
     matches ST_Number, which allows no `NaN`, `Inf` or `1e5`.
5. For files, write to a temp file and rename it (see `SaveFile` in
   [edit.md](edit.md)). Then load the output again and run `Validate` in tests.

## Rules

- Write ZIP entry names **without** a leading `/`. Part names and rel
  targets **do** start with `/`.
- Write vertices before triangles, and objects before the components or
  items that reference them. IDs are positive and unique per model part.
- Emit `requiredextensions` only for extensions the content truly needs
  to be understood. A p:UUID alone does not need it, but Bambu sets it
  anyway; see [bambu-layout.md](bambu-layout.md).
- Use `xml:lang` on `<model>`. Escape metadata text and attributes.
- Don't write `type="other"` objects into the build.
- To match stlTexturizer's output exactly, use `WeldProjectCompatible`
  and `FormatProjectCompatible`. These reproduce JS `Math.round` and
  `toFixed(4)`, which are lossy. Use them only for fixture parity, never
  as a default.

## References

- [generation-checklist.md](generation-checklist.md): the
  minimal package, annotated, with an XSD check and the project's export
  format.
- [library-alternatives.md](library-alternatives.md): the
  standard library vs go3mf vs lib3mf (cgo), and when to switch.

## Verify

```sh
go test ./... -race -run 'Generate|ProjectCompatible|JSRound|Weld|Example'
```

`TestGeneratedModelMatchesCoreXSD` validates output against the Core XSD
with `xmllint --nonet` (skipped if xmllint is missing). Related skills:
[validate.md](validate.md), go-testing.
