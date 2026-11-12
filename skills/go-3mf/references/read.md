# Reading 3MF in Go

A 3MF file is an OPC package (ZIP + `[Content_Types].xml` + `.rels`) holding
XML model parts. Read it in three layers and keep them separate:

1. **Package** – ZIP entries, content types, relationships.
   See [opc.go](../scripts/threemf/opc.go).
2. **Model parts** – parse each `.model` XML into objects, meshes, components
   and items, recording byte spans for later edits.
   See [model.go](../scripts/threemf/model.go).
3. **Resolution** – root discovery, cross-part references, transforms,
   units, flattening. See [document.go](../scripts/threemf/document.go)
   and [matrix.go](../scripts/threemf/matrix.go).

## Workflow

```go
pkg, err := threemf.OpenFile(path, threemf.DefaultLimits()) // ZIP + OPC
defer pkg.Close()
doc, err := threemf.Load(pkg)          // StartPart rel -> root -> referenced parts
insts, err := doc.Instances()          // build items expanded through components
tris, warns, err := doc.Flatten(threemf.FlattenOptions{}) // mm, world space
```

## Rules that loaders commonly get wrong

- **Root from relationships, not file names.** Follow the single StartPart
  relationship in `/_rels/.rels`. A missing or duplicate StartPart is an
  error. Don't scan every `*.model`; non-root parts come from the root
  part's `.rels` file (`/3D/_rels/3dmodel.model.rels`).
- **Object IDs are scoped per model part.** Key objects by `(part, id)`,
  because `object_1.model#1` and `3dmodel.model#1` can both exist.
- **Transforms use row vectors:** `x' = x*m00 + y*m10 + z*m20 + m30`.
  Compose child-first, as `world = component.Then(parent)`.
- **Units:** scale vertices by their own part's unit and translations by the
  unit of the part that holds the transform. Warn when parts disagree.
- **Negative determinant:** reverse triangle winding so normals stay outward.
- **Only emit what the build references.** If the build is empty, output
  nothing. Never fall back to "all meshes".
- **Object `type`:** by default flatten only `model` and `solidsupport`.
  Bambu modifiers and negative volumes are `type="other"` components.
- **`requiredextensions`:** if you can't process a listed namespace, refuse
  the file.
- **Strict numbers:** only the `ST_Number` grammar. Reject `1,5`, `NaN`,
  `Inf` and `1.`, and reject indices that are out of range.
- **Untrusted input:**
  - reject DTDs and ZIP paths with `..`, absolute paths or backslashes;
  - reject case-insensitive duplicate part names;
  - verify CRCs;
  - cap entries, part size, total bytes, triangles (after instancing) and
    component depth.
- **Production rules:** `p:path` is allowed only in the root part, and it
  must be absolute. Build items in non-root parts are ignored.

## References

- [package-anatomy.md](package-anatomy.md): OPC parts, content
  types, relationship types, part-name rules, layout of a minimal package.
- [transforms-units.md](transforms-units.md): matrix layout,
  composition order, unit table, mirrored winding, worked example.
- [bambu-layout.md](bambu-layout.md): Bambu Studio project
  structure, UUID patterns, paint attributes, `model_settings.config`.

## Verify

Run `go test ./... -race` in [scripts/](../scripts). The tests build in-memory ZIP
fixtures that cover a non-default root, mixed units, nested non-commuting
transforms, mirroring, cycles, limits and malformed input. To inspect a
real file:

```sh
go run ./cmd/inspect3mf -profile bambu file.3mf
```

Related: [edit.md](edit.md) (changing a loaded file), [validate.md](validate.md)
(conformance checks). For Go style and tests, follow go-idioms and
go-testing.
