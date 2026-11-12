package threemf_test

import (
	"bytes"
	"fmt"
	"log"

	"example.com/threemf/threemf"
)

// Generate a package, then load it, move one vertex and save it again while
// keeping every other byte of the package.
func Example() {
	verts := [][3]float64{{0, 0, 0}, {10, 0, 0}, {0, 10, 0}, {0, 0, 10}}
	tris := [][3]int{{0, 2, 1}, {0, 1, 3}, {0, 3, 2}, {1, 2, 3}}
	var buf bytes.Buffer
	err := threemf.Generate(&buf, []threemf.MeshObject{{Name: "tetra", Vertices: verts, Triangles: tris}},
		threemf.GenerateOptions{Metadata: []threemf.Metadata{{Name: "Title", Value: "demo"}}})
	if err != nil {
		log.Fatal(err)
	}

	pkg, err := threemf.OpenBytes(buf.Bytes(), threemf.DefaultLimits())
	if err != nil {
		log.Fatal(err)
	}
	doc, err := threemf.Load(pkg)
	if err != nil {
		log.Fatal(err)
	}
	obj := doc.Root.Objects[0]
	moved := append([][3]float64(nil), obj.Mesh.Vertices...)
	moved[3][2] = 12.5
	warnings, err := doc.SetVertices(doc.Root.Name, obj.ID, moved)
	if err != nil {
		log.Fatal(err)
	}
	var out bytes.Buffer
	if err := doc.Save(&out); err != nil {
		log.Fatal(err)
	}
	fmt.Println("root:", doc.Root.Name, "warnings:", len(warnings))
	fmt.Println("issues:", len(doc.Validate(threemf.ProfileStrict)))
	// Output:
	// root: /3D/3dmodel.model warnings: 0
	// issues: 0
}
