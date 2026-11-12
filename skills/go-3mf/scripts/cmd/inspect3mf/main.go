// Command inspect3mf prints the structure of a 3MF package: parts, content
// types, relationships, model parts, objects, build instances, load warnings
// and validation issues.
//
//	go run ./cmd/inspect3mf [-profile strict|bambu] file.3mf
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"example.com/threemf/threemf"
)

func main() {
	profile := flag.String("profile", "strict", "validation profile: strict or bambu")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: inspect3mf [-profile strict|bambu] file.3mf")
		os.Exit(2)
	}
	if err := run(os.Stdout, flag.Arg(0), *profile); err != nil {
		fmt.Fprintln(os.Stderr, "inspect3mf:", err)
		os.Exit(1)
	}
}

func run(w io.Writer, name, profile string) error {
	prof := threemf.ProfileStrict
	switch profile {
	case "strict":
	case "bambu":
		prof = threemf.ProfileBambu
	default:
		return fmt.Errorf("unknown profile %q", profile)
	}
	pkg, err := threemf.OpenFile(name, threemf.DefaultLimits())
	if err != nil {
		return err
	}
	defer pkg.Close()

	fmt.Fprintln(w, "== parts")
	for _, e := range pkg.Entries() {
		if e.IsDir {
			fmt.Fprintf(w, "  %s (directory entry)\n", e.Name)
			continue
		}
		fmt.Fprintf(w, "  %s  [%s]\n", e.Name, pkg.ContentType(e.Name))
	}
	fmt.Fprintln(w, "== relationships")
	rels := pkg.AllRels()
	srcs := make([]string, 0, len(rels))
	for s := range rels {
		srcs = append(srcs, s)
	}
	sort.Strings(srcs)
	for _, s := range srcs {
		for _, r := range rels[s] {
			fmt.Fprintf(w, "  %s -> %s  (%s)\n", s, r.Target, r.Type)
		}
	}

	doc, err := threemf.Load(pkg)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "== root model %s\n", doc.Root.Name)
	names := make([]string, 0, len(doc.Parts))
	for k := range doc.Parts {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		mp := doc.Parts[k]
		fmt.Fprintf(w, "== model part %s  unit=%s required=%v\n", mp.Name, mp.Unit, mp.RequiredExtensions)
		for _, m := range mp.Metadata {
			fmt.Fprintf(w, "  metadata %s = %q\n", m.Name, m.Value)
		}
		for _, o := range mp.Objects {
			switch {
			case o.Mesh != nil:
				fmt.Fprintf(w, "  object %d type=%s name=%q mesh: %d vertices, %d triangles, triangle attrs %v, extra %v\n",
					o.ID, o.Type, o.Name, len(o.Mesh.Vertices), len(o.Mesh.Triangles), o.Mesh.TriangleAttrs, o.Mesh.ExtraChildren)
			default:
				fmt.Fprintf(w, "  object %d type=%s name=%q components: %d\n", o.ID, o.Type, o.Name, len(o.Components))
			}
		}
	}
	insts, err := doc.Instances()
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "== build instances")
	for _, in := range insts {
		fmt.Fprintf(w, "  item %d: %s#%d type=%s mirrored=%v world=[%s]\n",
			in.Item, in.Part, in.Object.ID, in.Object.Type, in.Mirrored(), in.World)
	}
	fmt.Fprintln(w, "== validation")
	for _, is := range doc.Validate(prof) {
		fmt.Fprintf(w, "  %s\n", is)
	}
	return nil
}
