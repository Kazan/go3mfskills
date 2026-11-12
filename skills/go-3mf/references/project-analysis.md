# stlTexturizer 3MF analysis

This analysis covers the source repository
`CNCKitchen/stlTexturizer` at commit
`a2f8db6af96ecbec21e5861ccba673519f91b59e` (AGPL-3.0-only).

The specifications used were 3MF Core 1.4.0 and the Production Extension
1.2 (3MFConsortium `spec_core@997b385e06f3`, `spec_production@e7b7a5ab7051`), and Bambu Studio's
`src/libslic3r/Format/bbs_3mf.cpp` at `BambuStudio@930fe93851af` (read for
layout facts only).

Every claim is tagged with its source:
- **[Project]**: observed in the repo.
- **[Core]** or **[Prod]**: required by the spec.
- **[Bambu]**: read in Bambu Studio's source.
- **[Policy]**: a decision made for this bundle.

> **License note.** The Go examples in `scripts/` were written from the
> specs and this analysis. No JavaScript was translated line by line. The
> project's observable behaviour (number formatting, weld keys) is
> reproduced only where needed for byte-compatible fixtures.

## 1. How the project handles 3MF [Project]

### Import: `js/stlLoader.js`

| Step | Location | Behaviour |
|---|---|---|
| Size guard | `MAX_FILE_SIZE` :11 | 500 MB cap on the input file |
| Entry | `load3MFFile` :247 | `parse3MF`, then `setupGeometry` (:134), then `validateAndCleanGeometry` (:72); returns `originOffset` (the centring shift) |
| Limits | :270–271 | `MAX_3MF_TRIANGLES` = 10 000 000; `MAX_3MF_DEPTH` = 32 |
| Unzip and parse | `parse3MF` :275–278 | fflate `unzipSync` (whole archive in memory), `DOMParser` per part |
| Meshes | :311–347 | Parses **every** `*.model` entry; `objectMap["path#id"]`, and a later duplicate overwrites an earlier one; vertices via `parseFloat` into `Float32Array`; triangle indices checked with `/^[0-9]+$/` and `< vertexCount` |
| Root | :355 | First entry matching `3D/3dmodel.model` (case-insensitive), otherwise the first model |
| Units | :297–304, :363–364 | Root `unit` only, lowercased; an unknown unit scales by 1 |
| Transforms | `parseTransform` :370 | 12 numbers, transposed into a Three.js column-major `Matrix4`; any other count gives identity |
| Components | `resolveObject` :385–428 | Re-parses the part's XML on every call; descendant `<component>` search; `p:path` via namespaced *or* literal `p:path` attribute; prepends `/` unless the path starts with `/` or `3D`; `parent·component`; depth and cycle guard |
| Build | :430–451 | Root items only (item `p:path` ignored); no items or no instances means **every parsed mesh** is used with the unit scale applied |
| Output | :459+ | Triangle-count guard; a non-indexed `Float32` `BufferGeometry` in mm, world space |
| Cleanup | `validateAndCleanGeometry` :72 | Drops triangles that contain NaN or have \|(B−A)×(C−A)\|² < 1e-24 |
| Pose | `js/main.js` :4309–4320 | `currentPoseTrans = −originOffset`, so the model is centred in the app |

### Export: `js/exporter.js` and `js/zipStream.js`

| Step | Location | Behaviour |
|---|---|---|
| Pose restore | `main.js` `_restoreOriginalPose` :5542 | `orig = rot⁻¹·(mem − trans)` puts output back in the source file's world pose |
| Dispatch | `main.js` :5658–5662 | `export3MF(finalGeometry, name, abort)` |
| Weld | `export3MF` :104, :117 | `QuantizedPointMap(1e4)`: key `Math.round(c·1e4)`, first coordinates win (`js/meshIndex.js`) |
| Package | :133–153 | Fixed `[Content_Types].xml`; `_rels/.rels` with `Id="rel-1"` and absolute target `/3D/3dmodel.model` |
| Model | `modelChunks` :160–186 | Single object `id=1 type=model`, one item without transform, `unit="millimeter"`, `xml:lang="en-US"`, about 1 MB string chunks |
| Numbers | `fmt` :169 | `toFixed(4)`, then trailing zeros and `.` stripped (so `-0.00001` becomes `"-0"`) |
| ZIP | `zipChunks` `js/zipStream.js` :10, :27–30 | Streams with `CompressionStream('deflate-raw')`, falling back to fflate `ZipDeflate` level 6 |
| Tests | `tests/exportStreaming.test.mjs`, `tests/zipStream.test.mjs` | Streaming and ZIP framing |

**Consequence.** stlTexturizer is a **3MF → triangle soup → new 3MF**
pipeline. Nothing from the source package survives export:
- objects, names and build transforms are baked into a single object;
- metadata, materials and colours are lost;
- Bambu project settings, plates, paint and modifiers are lost.

This is the main gap the edit guide ([edit.md](edit.md)) fills.

## 2. Gaps in the project [Project vs Core/Prod]

| # | Project behaviour | Spec says | Impact | Go handling |
|---|---|---|---|---|
| 1 | Root guessed from the file name (:355) | Root is the StartPart target in `/_rels/.rels` [Core 2.1] | Wrong root in packages with renamed parts | `Load` follows the rel; requires exactly one |
| 2 | Parses every `*.model` (:311) | Only the root and the parts it references [Prod 2] | Unreachable leftovers are imported | Loads the root and its rels/`p:path` targets; reports `model-part-unreachable` |
| 3 | No items or no instances means all meshes are used (:439–451) | Only build items are printed [Core 3.4.3] | Hidden or helper meshes appear | Empty build gives no geometry |
| 4 | Object `type` ignored | `support`/`other` are not part of the printed solid [Core 4] | Bambu modifiers and negative volumes merge into the solid | `Flatten` defaults to model and solidsupport; warns `object-type-skipped` |
| 5 | No winding flip for mirrored transforms | Negative determinant inverts orientation [Core 3.3] | Inside-out normals for mirrored items | Flips winding when `det < 0` |
| 6 | Item `p:path` ignored | Items may reference other parts [Prod 3.1] | Missing objects | Supported |
| 7 | `requiredextensions` ignored | A consumer must refuse unsupported required extensions [Core 3.4] | Silent misinterpretation (beam lattice, slices, secure content) | `ErrUnsupportedRequire` |
| 8 | `parseFloat` is lenient (`"1abc"` gives 1) | ST_Number pattern [Core] | Garbage accepted | Strict regex |
| 9 | A wrong transform count becomes identity (:383) | Exactly 12 numbers [Core 3.3] | Silent misplacement | Error |
| 10 | Duplicate IDs overwrite (:347) | IDs are unique per part [Core 4] | Wrong mesh used | Error |
| 11 | Root unit applied to every part (:363) | Each model part has its own `unit` [Core 3.4]; Production does not say how units combine | Mis-scaled sub-parts | Per-part unit, `mixed-units` warning |
| 12 | Relative `p:path` gets `/` prepended unless it starts with `3D` | `p:path` must be absolute [Prod 3.1] | Invalid input accepted | Error |
| 13 | `Float32` storage | Vertices are `xs:double` | Precision loss over 16 M units | `float64` |
| 14 | A weld can collapse a sliver to a repeated index, which is written anyway | Triangles need 3 distinct vertices; mesh manifold [Core 4.1] | Non-conformant export (rare, since inputs are mostly pre-snapped) | `CheckMesh` rejects; drop collapsed triangles after welding |
| 15 | Whole archive inflated in memory | — | Memory spikes on large projects | `zip.Reader` is lazy; part and total budgets |
| 16 | No content-type, DTD or ZIP-path checks on import | OPC / Core 2.3.2 | Robustness | `Open` and `Load` enforce them |

## 3. Bambu Studio findings [Bambu]

These are details only. See
[bambu-layout.md](bambu-layout.md).

- **Root components and geometry parts.** The root `3D/3dmodel.model`
  holds components objects that reference
  `/3D/Objects/object_N.model` through `p:path`, plus the
  `requiredextensions="p"` setting.
- **Modifiers.** Modifier, negative and support volumes are written as
  `type="other"` (`bbs_3mf.cpp:8055`) and referenced as components. This
  violates Core 3.4.3.1 and 4.1, which say `other` objects MUST NOT be
  referenced from the build "either recursively or directly".
- **Paint.** Paint and segmentation are stored as **unprefixed**
  `paint_color`, `paint_supports`, `paint_seam`, `paint_fuzzy_skin` and
  `face_property` attributes on `<triangle>`, keyed by triangle index
  (:332–337).
- **Project metadata** lives in:
  - `Metadata/model_settings.config`: per-part matrix, source offsets,
    `mesh_stat face_count`, plates;
  - `project_settings.config`, `slice_info.config`;
  - `plate_N.gcode`/`.png`;
  - `/Auxiliaries/.thumbnails/*`, a dot segment that violates OPC
    part-name rules.
- **UUIDs.** The p:UUID suffixes `-61cb-…` (object), `-71cb-…` (shared
  mesh), `-81cb-…` (sub-object) and `-b206-…` (component), plus the fixed
  build UUID (:304–308), identify Bambu-written files.

## 4. Preservation policy vs MustPreserve [Policy vs Core]

Core only requires consumers to keep parts targeted by MustPreserve rels.
Other unknown parts may be dropped, and the spec says they *SHOULD NOT*
be preserved. The user's requirement is "never silently discard",
which is stricter, so the policy wins:
- every ZIP entry is copied unchanged (`zip.Writer.Copy`);
- edits are byte splices;
- anything the edit may have made stale is reported, not deleted.

See [preservation-contract.md](preservation-contract.md).

## 5. JS → Go mapping

| JS | Go (`scripts/threemf`) |
|---|---|
| `unzipSync` + `DOMParser` | `Open`/`OpenFile` (lazy `zip.Reader`, limits) + streaming `xml.Decoder` in `ParseModel` |
| root guess | `Load` via StartPart |
| `parseTransform` + `Matrix4` | `ParseMatrix`, `Matrix` (row-vector 3MF layout, `child.Then(parent)`) |
| `resolveObject` | `Document.Instances` (World, Chain, Mirrored) |
| `UNIT_TO_MM` | `UnitScale` |
| non-indexed `BufferGeometry` | `Document.Flatten` → `[][3][3]float64` in mm |
| `validateAndCleanGeometry` | caller's job; `CheckMesh` reports problems |
| `QuantizedPointMap(1e4)` weld | `WeldProjectCompatible` |
| `fmt` (`toFixed(4)`) | `FormatProjectCompatible`; the default is lossless `FormatNumber` |
| `export3MF` | `Generate` |
| — (absent) | `SetVertices`, `ReplaceMesh`, `SetItemTransform`, `SetComponentTransform`, `Save`/`SaveFile`, `Validate` |

## 6. Limitations of this bundle

- **No real Bambu Studio project was available**, so Bambu compatibility
  of edited files is **unverified**. The Bambu facts come from source
  reading and a synthetic fixture. Before relying on the edit path,
  re-open an edited project in Bambu Studio and re-slice it.
- Only Core and Production are modelled. Materials, beam lattice, slice,
  secure content and the rest are preserved but not interpreted, and
  their `requiredextensions` cause `Load` to reject the file.
- The examples do not add or remove parts, clone shared objects, rewrite
  `model_settings.config`, or handle signatures.
- Test coverage is 86.6%, below the 98% floor in go-testing. The examples
  illustrate the approach and are not a production library.
- `Document.Validate` is a conformance aid. lib3mf is the reference
  implementation.
