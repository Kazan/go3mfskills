# Validating 3MF in Go

Validation happens in three layers, and each catches different problems
([validate.go](../scripts/threemf/validate.go)):

| Layer | What | Fails as |
|---|---|---|
| `Load` | Things a reader cannot work around: ZIP and OPC safety, one StartPart, well-formed XML without DTD, root `<model>`, numbers and IDs, duplicate IDs, unsupported `requiredextensions`, `p:path` outside the root, limits | `error` |
| `Document.Validate(profile)` | Conformance problems a reader can survive | `[]Issue{Severity, Code, Part, Message}` |
| XSD (`xmllint`) | Model structure and attribute types | External tool (generated or edited parts) |

## Workflow

1. Run `pkg, err := threemf.OpenFile(name, threemf.DefaultLimits())` and
   then `doc, err := threemf.Load(pkg)`. An error means you cannot
   process the file safely. Report it and stop.
2. Run `issues := doc.Validate(threemf.ProfileStrict)` for spec
   conformance. Use `ProfileBambu` when the file is a Bambu Studio
   project and you want to know whether it is broken *beyond* what Bambu
   always writes.
3. Treat `SeverityError` in the chosen profile as non-conformant.
   Warnings are informational and include every `Load` warning.
4. After generating or editing, run validation again, and run
   `xmllint --nonet --schema testdata/3mf-core.xsd` on the changed model
   parts in tests.
5. Present issues grouped by part, with the code first, so they are easy
   to scan.

## What the Bambu profile downgrades

The Bambu profile turns the following errors into warnings, because Bambu
Studio writes them in every project:
- `dot-segment`: `/Auxiliaries/.thumbnails/...`;
- `unprefixed-extension-attribute`: `paint_color`, `paint_supports`,
  `paint_seam`, `paint_fuzzy_skin` and `face_property` on `<triangle>`;
- `build-includes-other`: modifier and negative volumes written as
  `type="other"` and referenced as components.

A **direct** `item-references-other` stays an error in both profiles.

## Rules

- Never "fix" a file during validation; validation is read-only.
- Mesh checks apply only to `model` and `solidsupport` objects. Core
  allows `support` and `other` meshes to be open.
- `CheckMesh` lists at most 5 problems per kind, then gives a count. Its
  output order depends on map iteration, so compare sets in tests, not
  slices.
- This is a conformance **aid**. For certification use lib3mf; see
  [library-alternatives.md](library-alternatives.md).

## References

- [validation-matrix.md](validation-matrix.md): every issue
  code, the rule it checks, its severity in each profile, and known gaps.

## Verify

```sh
go test ./... -race -run 'Bambu|Invalid|Malformed|Reject|XSD'
go run ./cmd/inspect3mf -profile bambu file.3mf
```
