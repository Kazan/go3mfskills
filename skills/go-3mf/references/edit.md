# Editing 3MF in Go while preserving everything else

**Policy.** Edit freely, never drop metadata silently, and report every
part of the package the edit may have made stale. Load first; see
[read.md](read.md).

## Model: splice the original bytes, don't re-serialize

Re-marshalling a model with `encoding/xml` structs loses:
- unknown elements and attributes (Bambu `paint_*`, vendor namespaces);
- namespace prefixes, comments and formatting.

Instead:

1. While parsing, record each editable element's byte span with
   `xml.Decoder.InputOffset()`: the offset before `Token()` returns the
   StartElement, and the offset after the matching EndElement. A UTF-8 BOM
   shifts every offset by 3 ([model.go](../scripts/threemf/model.go)).
2. Express an edit as `{start, end, replacement}` against the **original**
   bytes. A second edit of the same element replaces the first. Overlapping
   edits are an error ([edit.go](../scripts/threemf/edit.go)).
3. Write replacement elements with the element prefix the file already uses
   (`<c:vertices>` stays `c:`).
4. When saving ([write.go](../scripts/threemf/write.go)):
   - copy unchanged ZIP entries with `zip.Writer.Copy`, which keeps the same
     compressed bytes and CRC;
   - rewrite edited parts with their original name, method and attributes;
   - keep entry order and the archive comment;
   - write to a temp file, then `fsync`, then rename.

## Operations

| Call | Use when | Keeps | Warns |
|---|---|---|---|
| `SetVertices(part, id, v)` | Displacement or smoothing with the same vertex count | Triangles, triangle-indexed paint and materials | `triangle-attributes-unverified`, `vertex-attributes-dropped`, `shared-object` |
| `ReplaceMesh(part, id, v, t)` | Remeshing, subdivision or repair | Object element, UUID, other children | `triangle-attributes-dropped`, `mesh-extension-stale`, `mesh-too-small` |
| `SetItemTransform(i, m)` | Moving or rotating a placed item (root unit) | All other item attributes | Package staleness |
| `SetComponentTransform(part, id, ci, m)` | Moving a volume inside an object (that part's unit) | All other component attributes | Package staleness, `shared-object` |

Every edit also returns package-level warnings when these are present:
- `bambu-model-settings-stale`
- `slice-info-stale`
- `derived-artifacts-stale` (plate PNG and G-code)
- `thumbnails-stale`
- `signature-invalidated`

Show these warnings to the user. Don't delete the files.

## Rules

- **Coordinate space.** Edit coordinates are **object-local, in the
  part's unit**. If you edited flattened world-space triangles (as
  stlTexturizer does), convert them back to local space first: undo item
  and component transforms, undo mirroring and undo the unit. Alternatively,
  generate a new file ([write.md](write.md)) and say that slicer data was not
  carried over.
- **Shared objects.** One object placed several times changes every copy.
  `References()` counts instances; ask the user, or clone the object (out
  of scope for the examples).
- **Bambu files.** Geometry is in `/3D/Objects/object_N.model`. Edit the
  sub-object, not the root components object. Paint is stored per triangle
  index, so prefer `SetVertices` when topology is unchanged.
- **Validate inputs.** Reject NaN and Inf values, out-of-range indices and
  repeated indices before recording an edit.
- **Signed or protected packages.** Editing breaks signatures. Never
  re-save protected content unprotected.

## References

- [preservation-contract.md](preservation-contract.md): what
  "preserved" means at each level, and what is not guaranteed.
- [stale-metadata.md](stale-metadata.md): warning codes, which
  files each edit invalidates, and what a Bambu re-slice regenerates.

## Verify

```sh
go test ./... -race -run 'Set|Replace|Save|Edit'
```

The tests compare raw compressed bytes of every untouched entry, check that
bytes outside `<vertices>` are identical, and check that paint survives
`SetVertices`. Related: [validate.md](validate.md) (re-check after editing),
go-file-editing and go-testing (Go work rules).
