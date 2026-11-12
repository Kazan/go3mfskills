# Transforms, composition and units

## Matrix layout [Core 3.3]

`transform="m00 m01 m02 m10 m11 m12 m20 m21 m22 m30 m31 m32"`: 12 numbers,
and an absent attribute means identity. Points are **row vectors**
multiplied on the left:

```
[x' y' z' 1] = [x y z 1] · | m00 m01 m02 0 |
                           | m10 m11 m12 0 |
                           | m20 m21 m22 0 |
                           | m30 m31 m32 1 |
x' = x*m00 + y*m10 + z*m20 + m30
```

The translation is the last row (`m30 m31 m32`).

**Three.js conversion.** Three.js uses column vectors, so
`Matrix4.set(row-major)` must receive the transpose:
`set(v0,v3,v6,v9, v1,v4,v7,v10, v2,v5,v8,v11, 0,0,0,1)`. This is what
stlTexturizer's `parseTransform` does (js/stlLoader.js:370).

**Bambu `model_settings.config`.** The `<part>` metadata `matrix` is a 4x4
in the column-vector convention, written row-major. Don't parse it with
the 3MF reader [Bambu].

## Composition order

An item places object A. A has a component placing B with transform C. A
point p of B ends up at:

```
world(p) = p · C · I          // child transform first, then parent
Go:      C.Then(I)            // matrix.go: m.Then(next) applies m, then next
Three.js parent.clone().multiply(child)   // column-vector equivalent
```

Test this with **non-commuting** transforms, such as a rotation plus a
translation: if the composition order is wrong, the result is a different
point. In `TestComponentTransformOrder`, rotate90z(translate(10,0,0)) under
an item translate(0,0,5) maps the origin to `(0,10,5)`; the wrong order
gives `(10,0,5)`.

## Units [Core 3.4]

| unit | mm |
|---|---|
| micron | 0.001 |
| millimeter (default) | 1 |
| centimeter | 10 |
| inch | 25.4 |
| foot | 304.8 |
| meter | 1000 |

The values are case-sensitive XSD enumerations. **[Project]**
stlTexturizer lowercases the unit, falls back to scale 1 for unknown
values, uses only the root part's unit, and pre-multiplies a uniform
scale into every item transform.

The spec doesn't say how units combine across production parts.
**[Policy]**:
- vertices are scaled by **their own part's** unit;
- each transform's translation row is scaled by the unit of **the part
  that contains the transform attribute**;
- the linear 3x3 part is unitless;
- a `mixed-units` warning is emitted.

Bambu always writes millimetres, so this case is rare.

## Mirroring [Core 3.3]

If the 3x3 determinant is negative, the transform mirrors geometry. The
spec requires volume to stay positive, so a consumer swaps two corners of
each triangle, `(a,b,c) → (a,c,b)`. stlTexturizer doesn't do this (a gap).
`Flatten` does, and `TestMirroredItemKeepsOutwardWinding` checks it.

## Orientation [Core 4.1]

The outward normal is `(B−A)×(C−A)`, so triangles are counter-clockwise
when seen from outside. For a closed mesh, the signed volume
`Σ A·(B×C)/6` is positive.

## Numbers [Core XSD ST_Number]

```
^[+-]?(([0-9]+(\.[0-9]+)?)|(\.[0-9]+))([eE][+-]?[0-9]+)?$
```

- Rejected: `NaN`, `INF`, hex, `1.`, `1,5` and underscores. Go's
  `strconv.ParseFloat` accepts several of these, so match the regex first
  (matrix.go `ParseNumber`).
- Writing: `strconv.FormatFloat(v,'f',-1,64)` is the shortest form that
  round-trips. It never uses an exponent and never emits NaN or Inf.
- Resource IDs are 1..2^31−1, and indices are 0..2^31−1.
