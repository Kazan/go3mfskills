# Generation checklist

## Minimal conformant package [Core]

```text
[Content_Types].xml
_rels/.rels
3D/3dmodel.model
```

```xml
<!-- [Content_Types].xml -->
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/>
</Types>

<!-- _rels/.rels -->
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rel0" Target="/3D/3dmodel.model"
    Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>
</Relationships>

<!-- 3D/3dmodel.model -->
<model unit="millimeter" xml:lang="en-US"
       xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">
  <metadata name="Title">cube</metadata>
  <resources>
    <object id="1" type="model" name="cube">
      <mesh>
        <vertices><vertex x="0" y="0" z="0"/>…</vertices>
        <triangles><triangle v1="0" v2="2" v3="1"/>…</triangles>
      </mesh>
    </object>
  </resources>
  <build><item objectid="1"/></build>
</model>
```

## Checklist

| Item | Source | Notes |
|---|---|---|
| Exactly one StartPart rel in `/_rels/.rels` | Core 2.1 | `Load` rejects zero or several |
| Every part has a content type | OPC | Defaults by extension are enough |
| Rel `Id`s unique per `.rels` file; `Target` absolute | OPC | Relative targets are legal, but absolute avoids ambiguity |
| `unit` from the ST_Unit enum | Core 3.4 | micron, millimeter, centimeter, inch, foot, meter |
| Numbers match ST_Number | Core | No NaN or Inf, no leading `+`; exponent allowed |
| Resource IDs 1..2³¹−1, unique per part, defined before use | Core 4 | Components and items reference earlier objects |
| `model`/`solidsupport` meshes closed, manifold, outward | Core 4.1 | `CheckMesh` |
| Build items do not reference `type="other"`, directly or through components | Core 3.4.3.1 | — |
| Metadata names unique and well-known or prefixed | Core 3.4.1 | Declare the prefix's namespace |
| Production: `p:UUID` on build, items, objects, components; namespace in `requiredextensions` if `p:path` is used | Prod 1, 4 | `ProductionUUIDs` |
| Thumbnail: PNG/JPEG, content type, Thumbnail rel | Core 2.1 | `GenerateOptions.Thumbnail` |

## Schema check [Tooling]

[3mf-core.xsd](../scripts/threemf/testdata/3mf-core.xsd) is Core Appendix B.1 with two
local changes:
- it imports a local `xml.xsd` stub, so it works with `--nonet`;
- `maxOccurs="2147483647"` became `unbounded`, because libxml2 rejects
  large values.

The schema checks structure only. It does not check manifoldness,
references or OPC rules.

```sh
xmllint --nonet --noout --schema testdata/3mf-core.xsd 3dmodel.model
```

## stlTexturizer's export format [Project]

`js/exporter.js` `export3MF`:
- welds with `QuantizedPointMap(1e4)` (`Math.round(c*1e4)` keys; first
  coordinates win);
- writes `[Content_Types].xml`, `_rels/.rels` (Id `rel-1`, absolute
  target) and one object with one build item, no transform, unit
  millimeter;
- formats numbers with `toFixed(4)` and then strips trailing zeros, so
  `-0.00001` becomes `"-0"`;
- streams about 1 MB chunks through `CompressionStream('deflate-raw')`.

The Go reproductions are in
[projectcompat.go](../scripts/threemf/projectcompat.go), with values
checked against Node in `TestProjectCompatibleFormatting`. Project gap: a
weld can collapse a sliver triangle to a repeated index, which is written
anyway and violates Core 4.1.
