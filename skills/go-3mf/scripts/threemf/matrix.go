package threemf

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Matrix is a 3MF ST_Matrix3D: "m00 m01 m02 m10 m11 m12 m20 m21 m22 m30 m31 m32".
// 3MF uses row vectors, so a point p maps to p·M:
//
//	x' = x*m00 + y*m10 + z*m20 + m30
//
// (Core spec 3.3). The translation is stored in m30 m31 m32, in the unit of
// the model part that contains the transform attribute.
type Matrix [12]float64

// Identity returns the identity transform.
func Identity() Matrix { return Matrix{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0} }

// Translation returns a pure translation.
func Translation(x, y, z float64) Matrix { return Matrix{1, 0, 0, 0, 1, 0, 0, 0, 1, x, y, z} }

// Apply transforms a point.
func (m Matrix) Apply(p [3]float64) [3]float64 {
	x, y, z := p[0], p[1], p[2]
	return [3]float64{
		x*m[0] + y*m[3] + z*m[6] + m[9],
		x*m[1] + y*m[4] + z*m[7] + m[10],
		x*m[2] + y*m[5] + z*m[8] + m[11],
	}
}

// Then returns the transform that applies m first and next second. For a
// component inside a build item, world = component.Then(item).
func (m Matrix) Then(next Matrix) Matrix {
	var r Matrix
	for i := 0; i < 4; i++ { // row of m (row 3 is the translation row)
		for j := 0; j < 3; j++ {
			v := m[i*3]*next[j] + m[i*3+1]*next[3+j] + m[i*3+2]*next[6+j]
			if i == 3 {
				v += next[9+j]
			}
			r[i*3+j] = v
		}
	}
	return r
}

// Determinant of the linear 3x3 part. Negative means the transform mirrors,
// and triangle winding must be reversed to keep volume positive (Core 3.3).
func (m Matrix) Determinant() float64 {
	return m[0]*(m[4]*m[8]-m[5]*m[7]) - m[1]*(m[3]*m[8]-m[5]*m[6]) + m[2]*(m[3]*m[7]-m[4]*m[6])
}

// scaleTranslation converts the translation row to another unit.
func (m Matrix) scaleTranslation(s float64) Matrix {
	m[9], m[10], m[11] = m[9]*s, m[10]*s, m[11]*s
	return m
}

// String formats the matrix as an ST_Matrix3D attribute value.
func (m Matrix) String() string {
	var b strings.Builder
	for i, v := range m {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(FormatNumber(v))
	}
	return b.String()
}

// stNumber is the ST_Number lexical pattern from the Core XSD. It excludes
// forms that strconv.ParseFloat would accept: "NaN", "Inf", hex floats,
// underscores, "1." and locale commas.
var stNumber = regexp.MustCompile(`^[+-]?(([0-9]+(\.[0-9]+)?)|(\.[0-9]+))([eE][+-]?[0-9]+)?$`)

// ParseNumber parses an ST_Number (whitespace collapse applies).
func ParseNumber(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if !stNumber.MatchString(s) {
		return 0, fmt.Errorf("%w: invalid number %q", ErrMalformedModel, s)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%w: number %q out of range", ErrMalformedModel, s)
	}
	return v, nil
}

// FormatNumber writes the shortest decimal that round-trips the float64,
// without an exponent. It panics on NaN/Inf; validate input first.
func FormatNumber(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		panic("threemf: cannot format non-finite number")
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ParseMatrix parses an ST_Matrix3D. An empty string is the identity. Unlike
// the JavaScript reference parser, malformed values are errors, not identity.
func ParseMatrix(s string) (Matrix, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return Identity(), nil
	}
	if len(f) != 12 {
		return Matrix{}, fmt.Errorf("%w: transform needs 12 numbers, got %d", ErrMalformedModel, len(f))
	}
	var m Matrix
	for i, t := range f {
		v, err := ParseNumber(t)
		if err != nil {
			return Matrix{}, err
		}
		m[i] = v
	}
	return m, nil
}

// parseResourceID parses ST_ResourceID: a positive integer below 2^31.
func parseResourceID(s string) (int, error) {
	v, err := parseIndex(s)
	if err != nil || v == 0 {
		return 0, fmt.Errorf("%w: invalid resource id %q", ErrMalformedModel, s)
	}
	return v, nil
}

// parseIndex parses ST_ResourceIndex: a non-negative integer below 2^31.
func parseIndex(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.TrimLeft(s, "0123456789") != "" {
		return 0, fmt.Errorf("%w: invalid index %q", ErrMalformedModel, s)
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v >= 1<<31 {
		return 0, fmt.Errorf("%w: index %q out of range", ErrMalformedModel, s)
	}
	return int(v), nil
}

// UnitScale maps ST_Unit values to millimetres. Unit names are
// case-sensitive in the XSD enumeration.
var UnitScale = map[string]float64{
	"micron":     0.001,
	"millimeter": 1,
	"centimeter": 10,
	"inch":       25.4,
	"foot":       304.8,
	"meter":      1000,
}
