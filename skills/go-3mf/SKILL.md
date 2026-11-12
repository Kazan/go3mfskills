---
name: go-3mf
description: Read, edit, validate and generate 3MF (3D Manufacturing Format) files in Go with the standard library — OPC/ZIP packages, root discovery via relationships, Production p:path multi-file models, row-vector transforms and units, byte-preserving edits that keep Bambu Studio metadata and paint, stale-metadata warnings, closed/manifold mesh checks, strict and Bambu validation profiles, and spec-conformant export. Use when importing, inspecting, modifying, re-saving, linting or exporting a .3mf, handling Bambu Studio projects, explaining slicer rejections, or choosing between the standard library, go3mf and lib3mf.
license: AGPL-3.0-only
---

# 3MF in Go

A 3MF file is an OPC package (ZIP + `[Content_Types].xml` + `.rels`)
holding XML model parts. Work with it in three layers and keep them
separate: **package** (ZIP, content types, relationships), **model parts**
(objects, meshes, components, items, with byte spans for edits), and
**resolution** (root, cross-part references, transforms, units).

## Pick the task guide

| Task | Read |
|---|---|
| Open a .3mf, find the root, resolve components, transforms and units, extract triangles | [references/read.md](references/read.md) |
| Change geometry or transforms in an existing .3mf, keep everything else, report stale metadata | [references/edit.md](references/edit.md) |
| Generate a new .3mf from meshes, match stlTexturizer's export, choose a library | [references/write.md](references/write.md) |
| Check conformance (strict or Bambu), run the XSD, explain slicer rejections | [references/validate.md](references/validate.md) |

Editing and validating assume you have loaded the file as described in
read.md.

## Supporting references

| File | Contents |
|---|---|
| [package-anatomy.md](references/package-anatomy.md) | OPC parts, content types, relationship types, part-name rules, minimal package |
| [transforms-units.md](references/transforms-units.md) | Matrix layout, composition order, units, mirrored winding |
| [bambu-layout.md](references/bambu-layout.md) | Bambu Studio project structure, UUIDs, paint attributes, config files |
| [preservation-contract.md](references/preservation-contract.md) | What "preserved" means at each level when editing |
| [stale-metadata.md](references/stale-metadata.md) | Warning codes and what each edit invalidates |
| [generation-checklist.md](references/generation-checklist.md) | Annotated minimal package, XSD check, stlTexturizer export format |
| [library-alternatives.md](references/library-alternatives.md) | Standard library vs go3mf vs lib3mf |
| [validation-matrix.md](references/validation-matrix.md) | Every issue code, rule, and severity per profile |
| [project-analysis.md](references/project-analysis.md) | How stlTexturizer handles 3MF, its gaps against the spec, JS → Go mapping |

## Reference implementation

[scripts/](scripts) is a standalone Go module (`example.com/threemf`,
Go 1.22+, no dependencies) implementing everything above. Read the file
for the task rather than the whole module:

| File | Covers |
|---|---|
| [opc.go](scripts/threemf/opc.go) | ZIP/OPC package, limits, safe names, relationships |
| [model.go](scripts/threemf/model.go) | Streaming model parser with byte spans |
| [matrix.go](scripts/threemf/matrix.go) | `ST_Matrix3D`, `ST_Number`, units |
| [document.go](scripts/threemf/document.go) | Root discovery, multi-part loading, instances, flattening |
| [edit.go](scripts/threemf/edit.go) | `SetVertices`, `ReplaceMesh`, `SetItemTransform`, `SetComponentTransform` |
| [write.go](scripts/threemf/write.go) | `Save` / `SaveFile` with raw copy of untouched entries |
| [validate.go](scripts/threemf/validate.go) | `Validate` (strict / Bambu), `CheckMesh` |
| [generate.go](scripts/threemf/generate.go) | `Generate` new packages |
| [projectcompat.go](scripts/threemf/projectcompat.go) | stlTexturizer-compatible weld and number format |

Run it from `scripts/`:

```sh
go test ./... -race -count=1                     # xmllint optional
go run ./cmd/inspect3mf -profile bambu file.3mf  # parts, rels, instances, issues
```

For general Go style and tests, follow go-idioms, go-file-editing and
go-testing.
