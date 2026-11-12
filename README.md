# 3MF in Go: agent plugin

This agent plugin covers reading, editing, validating and generating 3MF
(3D Manufacturing Format) files in Go. It was extracted from an
analysis of
[stlTexturizer](https://github.com/CNCKitchen/stlTexturizer) at
`a2f8db6`, the 3MF Core 1.4.0 and Production 1.2 specs, and Bambu
Studio's 3MF code.

## Contents

The plugin ships one skill, [go-3mf](skills/go-3mf/SKILL.md). Its
`SKILL.md` sends the agent to a task guide (read, edit, write or validate)
and lists the supporting references:

```text
three-mf-go-skills/            plugin root
  plugin.json                  manifest
  skills/go-3mf/
    SKILL.md                   router: task guides, references, code map
    references/                read/edit/write/validate guides + 9 references
                               (project-analysis.md: stlTexturizer analysis)
    scripts/                   Go module example.com/threemf + cmd/inspect3mf
```

This follows the [Agent Skills layout](https://agentskills.io/specification):
everything the skill links to sits inside its own folder, so the skill
works whether it is installed as a plugin or copied on its own. The Go code
is a single package whose parts depend on each other (edit uses read and
write), which is why there is one skill rather than four.

## Install

As a plugin:

```sh
copilot plugin install OWNER/REPO:three-mf-go-skills
```

In VS Code, use `Chat: Install Plugin From Source`. Alternatively, copy
`skills/go-3mf/` into `~/.copilot/skills/` or a repository's
`.github/skills/`.

## Running the code

```sh
cd skills/go-3mf/scripts
gofmt -l . && go vet ./...
go test ./... -race -count=1          # xmllint optional (XSD test skips without it)
go run ./cmd/inspect3mf -profile bambu path/to/file.3mf
```

[SKILL.md](skills/go-3mf/SKILL.md) maps each Go file to what it covers.

## Verification status

- `gofmt`, `go vet` and `go test ./... -race -count=1` pass on Go 1.27.3.
- Generated models validate against the Core XSD with `xmllint --nonet`.
- Number formatting matches values produced by running the project's
  `fmt` in Node.
- Statement coverage is **86.6%**. This is below the go-testing floor of
  98%, because the module illustrates the approach and is not a
  production library. Raise coverage before adopting it.

## Known gaps

- **Bambu Studio interoperability is unverified.** No real Bambu project
  was available, so tests use a synthetic fixture modelled on
  `bbs_3mf.cpp`. Round-trip a real project, re-open it in Bambu Studio
  and re-slice it before relying on the edit path.
- Only Core and Production are interpreted. Other extensions are
  preserved, but files that *require* them are rejected.
- The examples cannot add or remove parts, clone shared objects, rewrite
  `model_settings.config`, or handle signatures.
- `CheckMesh` problem order is nondeterministic (map iteration).

## Provenance and license

- **Project:** stlTexturizer `a2f8db6af96e…` (AGPL-3.0-only), observed and
  not copied.
- **Specs:** `spec_core@997b385e06f3`, `spec_production@e7b7a5ab7051`.
- **Bambu:** `BambuStudio@930fe93851af`.
- **XSD:** `scripts/threemf/testdata/3mf-core.xsd` is Core Appendix B.1, with the local
  edits noted in its header.

The Go code was written from the specs. The bundle lives in the
stlTexturizer repository, so the repository's [LICENSE](../LICENSE)
(AGPL-3.0-only) applies unless the maintainers decide otherwise.
