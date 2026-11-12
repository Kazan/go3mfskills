# Preservation contract

This is **[Policy]** for the Go examples. The 3MF spec only requires that
consumers keep MustPreserve parts; see
[package-anatomy.md](package-anatomy.md).

## Guarantee levels

| Level | Guarantee | Mechanism | Test |
|---|---|---|---|
| Untouched entries | Compressed payload, CRC, method, name, comment and extra fields are bit-identical | `zip.Writer.Copy(f)` | `TestSetVerticesPreservesPackage` compares `OpenRaw` bytes |
| Entry set and order | Same names, same order, directory entries included, archive comment kept | Iterate `pkg.Entries()` in archive order | Same test |
| Edited model part, outside edits | Bytes before and after each edited element are identical: XML declaration, BOM, namespaces, unknown attributes, comments, whitespace, other objects | Byte splices against the original | Same test, plus `TestReplaceMeshWarnsAndDropsTriangleAttrs` and `TestEditKeepsPrefixAndBOM` |
| Edited element | Semantics preserved: numbers round-trip exactly (`FormatFloat 'f' -1`) and prefixes are kept | `writeVertices` / `setStartTagAttr` | Exact `float64` comparison after reload |
| Container bytes | **Not guaranteed**: local header timestamps of rewritten entries, data descriptors, ZIP64 layout, central directory offsets | Go's `archive/zip` writer | n/a |

## What an edit intentionally changes

- **`SetVertices`** rewrites `<vertices>`, so each `<vertex>` keeps only
  x/y/z. Extra vertex attributes are reported with
  `vertex-attributes-dropped`. Core defines none, so they would be vendor
  data.
- **`ReplaceMesh`** rewrites `<vertices>` and `<triangles>`. Triangle
  attributes (`p1/p2/p3/pid`, Bambu `paint_*`, `face_property`) are dropped
  and reported. Attributes on the `<triangles>` element itself are also
  replaced. Children such as `<t:trianglesets>` are kept but reported,
  because they hold triangle indices.
- **Transform edits** rewrite one start tag. Other attributes keep their
  bytes, quoting and order. If there was no transform, it is appended
  before `/>` or `>`.

## Not supported by the examples (extend deliberately)

- **Adding or removing parts.** This also requires updating
  `[Content_Types].xml` and `.rels` (model parts need a StartPart-type rel
  from the root part's `.rels`).
- **Cloning a shared object** before editing a single instance.
- **Rewriting Bambu `model_settings.config`.** `face_count`, the part
  matrix and the source offsets go stale. Bambu Studio recomputes most of
  this when the project is re-opened and re-sliced, but that is unverified
  without a real round-trip test.
- **Re-signing, or anything involving protected content.**

## Why not round-trip through structs or another library

- `encoding/xml` struct round-trips drop anything without a field. With
  `,any` and `,any,attr` catch-alls they keep content but rewrite
  prefixes and namespace declarations, which hurts byte-level diffs and
  some strict slicers.
- github.com/hpinc/go3mf (BSD-2-Clause) models extensions and claims to
  round-trip unknown ones. It stores vertices as `float32`, and its
  handling of **unprefixed** Bambu `paint_*` attributes is unverified.
  Test it on real Bambu files before you rely on it for preservation; see
  [library-alternatives.md](library-alternatives.md).
