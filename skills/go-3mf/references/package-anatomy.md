# 3MF package anatomy (OPC)

Each statement is tagged with its source:
- **[Core]**: 3MF Core spec v1.4.0 (spec_core@997b385e06f3).
- **[Prod]**: Production extension v1.2 (spec_production@e7b7a5ab7051).
- **[OPC]**: Open Packaging Conventions rules that 3MF adopts.
- **[Bambu]**: Bambu Studio `bbs_3mf.cpp` (BambuStudio@930fe93851af).
- **[Policy]**: a choice made by the Go examples.

## Minimal package [Core]

```
[Content_Types].xml        content-type map (Default by extension, Override by part)
_rels/.rels                package relationships; holds the StartPart
3D/3dmodel.model           root 3D model part (any name is allowed; the rel decides)
```

`[Content_Types].xml`:

```xml
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels"  ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/>
  <Default Extension="png"   ContentType="image/png"/>
</Types>
```

`_rels/.rels`:

```xml
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rel0" Target="/3D/3dmodel.model"
    Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>
</Relationships>
```

## Relationship types

| Constant in opc.go | Type URI | Rule |
|---|---|---|
| RelTypeStartPart | `http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel` | Exactly one in `/_rels/.rels` [Core]. Non-root model parts use the same type from the root part's `.rels` [Prod]. |
| RelTypeThumbnail | `http://schemas.openxmlformats.org/package/2006/relationships/metadata/thumbnail` | Package or object thumbnail. The target must exist [Core]. |
| RelTypePrintTicket | `http://schemas.microsoft.com/3dmanufacturing/2013/01/printticket` | The target must exist [Core]. |
| RelTypeMustPreserve | `http://schemas.openxmlformats.org/package/2006/relationships/mustpreserve` | Parts a consumer must keep when editing [Core]. |
| RelTypeSignatureOrigin | `…/digital-signature/origin` | Digital signatures. Editing invalidates them [Core]. |
| (Bambu) | `http://schemas.bambulab.com/package/2021/cover-thumbnail-middle`, `…-small` | Plate cover thumbnails [Bambu]. |

## Relationship resolution [OPC]

- The `.rels` file for part `/A/b.model` is `/A/_rels/b.model.rels`. The
  package's own rels are in `/_rels/.rels`, with source `/`.
- A relative `Target` resolves against the **source part's folder**:
  `inch.model` in `/3D/_rels/root.model.rels` becomes `/3D/inch.model`.
- `TargetMode="External"` targets are URIs, not parts.
- Part names compare case-insensitively. Two ZIP entries that differ only in
  case make the package invalid.

## Content types [OPC]

An `Override PartName=…` match comes first, then `Default Extension=…`.
Both are case-insensitive. Every part except `[Content_Types].xml` needs a
content type. Model parts must use the 3D model content type.

## Part names [Core/OPC]

- Names are absolute (`/3D/x.model`) and stored in the ZIP without the
  leading `/`. There are no empty, `.` or `..` segments and no backslashes.
- Segments must not start with a period. **[Bambu]** deviates with
  `/Auxiliaries/.thumbnails/`. The validate skill grades this.
- XML must be UTF-8 and DTDs are forbidden. An attribute from an extension
  namespace must be prefixed. Unknown namespaces must be ignored, not
  rejected.

## MustPreserve vs preserve-everything

**[Core]** says custom parts without a MustPreserve relationship "SHOULD
NOT" be kept by an editing consumer. **[Policy]** The examples copy
**every** entry anyway, because the user requirement is to keep slicer
metadata such as Bambu `Metadata/*.config`, which has no MustPreserve
relationship. Bambu reads those files back, so dropping them would lose
plates and settings. Keeping parts is never a conformance error for a
consumer of the output.

## Security limits [Policy]

`DefaultLimits()`:
- 10k entries;
- 1 GiB per part;
- 4 GiB total read;
- 50M triangles, counted after instancing in `Flatten`;
- component depth 32.

Check the ZIP's declared size **and** cap the actual read, because the
header can lie. CRC mismatches surface as `zip.ErrChecksum`.
