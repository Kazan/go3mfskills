# Validation matrix

Spec versions: Core 1.4.0, Production 1.2. Severity: **E** = error, **W** = warning, **D** = deviation (an error in
Strict, a warning in Bambu). The "Rule" column cites the spec section the
check comes from.

## `Document.Validate` issues

| Code | Rule | Strict | Bambu | Checks |
|---|---|---|---|---|
| `missing-content-type` | OPC / Core 2 | E | E | Each non-directory entry has a Default or Override content type |
| `dot-segment` | OPC part names | D | D | No segment starts with `.` (`/_rels/.rels` is exempt) |
| `rel-target-missing` | Core 2.1 | E for StartPart, Thumbnail, PrintTicket; W otherwise | same | Internal rel targets exist |
| `model-part-unreachable` | Prod 2 | W | W | A model-typed part was neither the root nor referenced |
| `production-not-required` | Prod 1 | E | E | `p:path` is used but the production namespace is not in the root's `requiredextensions` |
| `duplicate-metadata` | Core 3.4.1 | E | E | Metadata names are unique per model |
| `unknown-metadata` | Core 3.4.1 | W | W | Unprefixed names must be well-known |
| `non-root-metadata` | Prod 3.2 | W | W | Metadata in non-root parts |
| `missing-uuid` | Prod 4 | E | E | With `p` required: p:UUID on build, items, objects, components |
| `components-with-pid` | Core 4 | E | E | A components object must not carry `pid` |
| `missing-object` | Core 4 | E | E | Same-part component references resolve |
| `forward-reference` | Core 4 | E | E | Referenced object is defined earlier |
| `mesh` | Core 4.1 | E | E | `CheckMesh` on `model`/`solidsupport` meshes |
| `unprefixed-extension-attribute` | Core 2.3.3.1 | D | D | Non-core attributes on `<triangle>`/`<vertex>` without a namespace |
| `build-unresolvable` | Core 3.4.3 | E | E | `Instances()` failed (missing object, cycle, depth) |
| `item-references-other` | Core 3.4.3.1, 4.1 | E | E | A build item directly targets `type="other"` |
| `build-includes-other` | Core 3.4.3.1, 4.1 ("directly or recursively") | D | D | `type="other"` is reached from the build through components |
| Load warning codes | — | W | W | Passed through; see [stale-metadata.md](stale-metadata.md) |

## Enforced by `Load` (hard errors)

These are not `Issue`s:
- ZIP: limits, CRC, duplicate names, unsafe names (`..`, absolute paths,
  backslashes).
- OPC: missing `[Content_Types].xml`, a DTD in any parsed XML part
  (content types, rels, models).
- Exactly one internal StartPart.
- Model XML:
  - malformed XML, or a root element that is not core `<model>`;
  - an unknown unit or object type;
  - an invalid ST_Number, ID or index;
  - a transform without 12 numbers;
  - a triangle index out of range;
  - duplicate resource IDs.
- Extensions: a `requiredextensions` entry that is unsupported or uses an
  undeclared prefix; `p:path` in a non-root part.
- Limits: triangle and part-size budgets; component depth and cycles (in
  `Instances`).

## Not checked (known gaps)

- Materials extension (`basematerials`, `colorgroup`, `pid`/`pindex`
  consistency): IDs are only collected for duplicate detection.
- `p:UUID` format and uniqueness; whether thumbnail PNG/JPEG content
  matches its declared type.
- Self-intersection; mesh checks on `support` and `other` meshes (Core
  allows these to be open).
- Beam lattice, slice, secure content, volumetric and other extensions:
  their content is preserved but not validated, and a file that
  *requires* one of them is rejected by `Load`.
- Digital signatures: their presence is reported when edits invalidate
  them, but they are never verified.

## XSD layer

`testdata/3mf-core.xsd` checks element order, required attributes and
types in a single model part. Run it only on parts you wrote; Bambu parts
fail it on purpose because of their unprefixed `paint_*` attributes.
