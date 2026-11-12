package threemf

import (
	"math"
	"math/big"
	"strings"
)

// This file reproduces the stlTexturizer JavaScript exporter (js/exporter.js,
// AGPL-3.0-only; behaviour re-implemented here from observation, not copied)
// for callers who need byte-compatible output with that project. New code
// should prefer Generate with FormatNumber, which is lossless.

// jsRound is JavaScript Math.round: nearest integer, halves toward +Inf.
// math.Floor(x+0.5) is wrong for 0.49999999999999994 (rounds to 1).
func jsRound(x float64) float64 {
	r := math.Floor(x)
	if x-r >= 0.5 {
		r++
	}
	return r
}

// WeldProjectCompatible welds a non-indexed float32 triangle soup the way the
// project does: vertices whose Math.round(c*1e4) keys match on all three
// axes are merged, and the first occurrence's coordinates are kept.
//
// Like the project, it does not drop triangles that collapse when two of
// their corners weld together; run CheckMesh on the result before writing
// (Core forbids repeated vertex indices).
func WeldProjectCompatible(positions []float32) ([][3]float64, [][3]int) {
	type key [3]float64
	index := make(map[key]int)
	var verts [][3]float64
	tris := make([][3]int, len(positions)/9)
	for i := range tris {
		for j := 0; j < 3; j++ {
			b := i*9 + j*3
			p := [3]float64{float64(positions[b]), float64(positions[b+1]), float64(positions[b+2])}
			k := key{jsRound(p[0] * 1e4), jsRound(p[1] * 1e4), jsRound(p[2] * 1e4)}
			idx, ok := index[k]
			if !ok {
				idx = len(verts)
				index[k] = idx
				verts = append(verts, p)
			}
			tris[i][j] = idx
		}
	}
	return verts, tris
}

var tenThousand = new(big.Float).SetPrec(256).SetInt64(10000)

// FormatProjectCompatible matches the project's fmt(): Number.toFixed(4),
// then trailing zeros and a trailing "." removed. toFixed rounds the exact
// binary value and resolves exact ties upward in magnitude, so 1.03125 gives
// "1.0313" (strconv.FormatFloat(1.03125, 'f', 4, 64) gives "1.0312").
// Small negatives keep their sign: -0.00001 gives "-0". Values with
// |v| >= 1e21, which toFixed prints in exponent form, are not supported.
func FormatProjectCompatible(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) >= 1e21 {
		panic("threemf: FormatProjectCompatible needs a finite value below 1e21")
	}
	neg := v < 0 // toFixed: (-0).toFixed(4) is "0.0000"
	x := new(big.Float).SetPrec(256).SetFloat64(math.Abs(v))
	x.Mul(x, tenThousand) // exact: 53-bit mantissa times 14-bit integer
	n, _ := x.Int(nil)    // floor, since x >= 0
	frac := new(big.Float).SetPrec(256).Sub(x, new(big.Float).SetPrec(256).SetInt(n))
	if frac.Cmp(big.NewFloat(0.5)) >= 0 {
		n.Add(n, big.NewInt(1))
	}
	s := n.String()
	for len(s) < 5 {
		s = "0" + s
	}
	s = s[:len(s)-4] + "." + s[len(s)-4:]
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	if neg {
		s = "-" + s
	}
	return s
}
