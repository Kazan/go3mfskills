# Bambu Studio 3MF layout

Source: `src/libslic3r/Format/bbs_3mf.cpp` at BambuStudio@930fe93851af
(read for behaviour, not copied). **No real Bambu export was available
while this was written.** The test fixtures are synthetic, built from the
source code, so treat interoperability as unverified until you test with
real files.

## Parts

| Part | Content | Edit impact |
|---|---|---|
| `/3D/3dmodel.model` | Root. `xmlns:BambuStudio="http://schemas.bambulab.com/package/2021"`, `xmlns:p` (production), `requiredextensions="p"`. Metadata `BambuStudio:3mfVersion` (legacy `bamboo_slicer:Version3mf`) identifies a Bambu file. Root objects contain only `<components>` with `p:path`. | Transforms live here. |
| `/3D/_rels/3dmodel.model.rels` | StartPart-type rels to every `/3D/Objects/object_N.model`. | Keep. |
| `/3D/Objects/object_N.model` | Mesh objects (volumes), one sub-object per volume. | Geometry edits go here. |
| `/Metadata/model_settings.config` | XML: per-object `<object id=rootId>` metadata (name, extruder, …), `<part id=subId subtype=…>` holding `matrix`, `source_*`, `<mesh_stat face_count…>`; `<plate>` with `<model_instance>` (object_id, instance_id, identify_id) and `<assemble>`. | Goes stale on geometry or transform edits. |
| `/Metadata/project_settings.config` | JSON print/filament/printer settings. | Unaffected by geometry. |
| `/Metadata/slice_info.config`, `/Metadata/plate_N.gcode` (+`.md5`), `/Metadata/plate_N.png`, `/Metadata/bbl_thumbnail.png`, files named by `<plate>` attributes `thumbnail_file`, `top_file`, `pick_file`, `pattern_bbox_file` | Slicing and rendering results. | Go stale on any geometry edit. |
| `/Auxiliaries/.thumbnails/thumbnail_{3mf,middle,small}.png` | Cover thumbnails (dot-segment deviation). | Go stale. |
| `/Metadata/cut_information.xml`, `layer_heights_profile.txt`, `layer_config_ranges.xml`, `brim_ear_points.txt`, `custom_gcode_per_layer.xml`, `filament_sequence.json`, `assembly_*.json`, `print_profile.config`, embedded presets `print_setting_*`, `process_settings_*`, `filament_settings_*`, `machine_settings_*`, `/Metadata/_rels/model_settings.config.rels` | Per-object/plate auxiliary data and presets. | Height- or position-keyed data goes stale on scale/transform edits. |

## Object and volume types

- Model-part volumes are `type="model"`.
- Modifiers, negative volumes, support blockers and support enforcers are
  written as `type="other"` but are still referenced as components (`bbs_3mf.cpp:8055`) of the
  root object.
  - Core says items must not reference `type="other"`, even through
    components. This is a Bambu deviation.
  - `model_settings.config` `<part subtype=…>` gives the real role, parsed with
    `ModelVolume::type_from_string`. The subtype strings are defined in
    Bambu's `Model.cpp`, which wasn't checked here.

## UUID patterns (p:UUID)

| Element | Pattern |
|---|---|
| root object | `%08x-61cb-4c03-9d28-80fed5dfa1dc` (`-71cb-…` when the mesh is shared) |
| sub-object (volume) | `%08x-81cb-4c03-9d28-80fed5dfa1dc` |
| component | `%08x-b206-40ff-9872-83e8017abed1` |
| build | `2c7c17d8-22b5-4d84-8835-1976022ea369` |

Keep existing UUIDs when editing. New objects need new UUIDs (crypto/rand
v4 is fine).

## Per-triangle attributes

`<triangle>` carries **unprefixed** attributes: `paint_supports`,
`paint_seam`, `paint_color` (MMU), `paint_fuzzy_skin` and `face_property`.

- Core requires extension attributes to be namespaced, so this is a
  deviation that the validate skill grades.
- Bambu reads the attributes back **by triangle index**:
  - Moving vertices without changing triangle order keeps them attached
    (`SetVertices`).
  - Changing topology makes them meaningless. `ReplaceMesh` drops them
    with a warning.
- The values are serialized paint trees. Don't edit them by hand.

## Matrix and offsets

`<part>` metadata `matrix` is a 4x4 written row-major in the column-vector
convention, the transpose of the 3MF layout. `source_offset_x/y/z` record
the volume's offset in its source object. After a transform edit, these
and `mesh_stat face_count` can disagree with the model part. The edit
skill reports this rather than rewriting the file.
