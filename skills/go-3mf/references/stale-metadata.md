# Stale metadata and warning codes

Edits return `[]Warning{Code, Part, Message}`, and `Load` adds load-time
warnings to `Document.Warnings`. Codes are stable strings, so callers can
filter them or turn them into UI messages.

## Edit warnings

| Code | Raised by | Meaning | Suggested user action |
|---|---|---|---|
| `shared-object` | SetVertices, ReplaceMesh, SetComponentTransform | More than one build instance uses the object, so all of them change | Confirm, or clone the object first |
| `triangle-attributes-unverified` | SetVertices | Paint/material attributes are still attached by index, but now sit on moved geometry | Check the paint in the slicer |
| `triangle-attributes-dropped` | ReplaceMesh | Paint/material attributes were removed | Repaint in the slicer |
| `vertex-attributes-dropped` | SetVertices, ReplaceMesh | Non-core vertex attributes were not rewritten | Usually none |
| `mesh-extension-stale` | ReplaceMesh | Foreign `<mesh>` children (triangle sets, …) kept old indices | Regenerate or remove them deliberately |
| `mesh-too-small` | ReplaceMesh | A `model` mesh has fewer than 4 triangles | Fix the mesh |
| `bambu-model-settings-stale` | every edit, if `/Metadata/model_settings.config` exists | face_count, part matrix, source offsets and plate placement may not match | Re-open and re-save in Bambu Studio |
| `slice-info-stale` | every edit | `slice_info.config` describes old geometry | Re-slice |
| `derived-artifacts-stale` | every edit | `Metadata/plate_*` previews and `*.gcode` (+md5) are from old geometry | Re-slice before printing; never print the embedded G-code |
| `thumbnails-stale` | every edit | Thumbnail relationship targets and `/Auxiliaries/.thumbnails/*` show old geometry | Regenerate (`Package.ReplacePart`) or ignore |
| `signature-invalidated` | every edit, if signature rels exist | Digital signatures no longer verify | Re-sign or tell the user |

## Load warnings

| Code | Meaning |
|---|---|
| `model-part-not-in-rels` | Reached through `p:path` but not declared in the root part's `.rels` (Production requires the rel). Followed anyway. |
| `non-root-build-ignored` | A non-root model part has build items. They are ignored, as Production specifies. |
| `mixed-units` | A non-root part's unit differs from the root's; see [transforms-units.md](transforms-units.md). |
| `model-content-type` | A model part has an unexpected content type. |
| `object-type-skipped` (Flatten) | Instances of excluded types (for example Bambu modifiers, `type="other"`) were left out of the flattened triangles. |

## Why stale data is reported, not deleted

Deleting `plate_1.gcode` would stop users from printing outdated G-code,
but it would also break Bambu features that expect the file. Removing a
thumbnail breaks its relationship target, which Core treats as an error.
The edit layer cannot regenerate these files faithfully, so it keeps them
and tells the caller. A caller that *can* regenerate a thumbnail uses
`Package.ReplacePart`.
