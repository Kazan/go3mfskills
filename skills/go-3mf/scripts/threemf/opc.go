// Package threemf is a small, standard-library-only example of reading,
// editing, saving and generating 3MF packages. It is teaching material for
// the go-3mf-* skills, not a complete 3MF SDK.
package threemf

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
)

// Relationship types, namespaces and content types from the 3MF Core and
// Production specifications (Core Appendix C, Production Appendix D) and OPC.
const (
	RelTypeStartPart       = "http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"
	RelTypeThumbnail       = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/thumbnail"
	RelTypePrintTicket     = "http://schemas.microsoft.com/3dmanufacturing/2013/01/printticket"
	RelTypeMustPreserve    = "http://schemas.openxmlformats.org/package/2006/relationships/mustpreserve"
	RelTypeSignatureOrigin = "http://schemas.openxmlformats.org/package/2006/relationships/digital-signature/origin"

	NSCore          = "http://schemas.microsoft.com/3dmanufacturing/core/2015/02"
	NSProduction    = "http://schemas.microsoft.com/3dmanufacturing/production/2015/06"
	NSTriangleSets  = "http://schemas.microsoft.com/3dmanufacturing/trianglesets/2021/07"
	NSContentTypes  = "http://schemas.openxmlformats.org/package/2006/content-types"
	NSRelationships = "http://schemas.openxmlformats.org/package/2006/relationships"

	ContentTypeModel = "application/vnd.ms-package.3dmanufacturing-3dmodel+xml"
	ContentTypeRels  = "application/vnd.openxmlformats-package.relationships+xml"
	ContentTypePNG   = "image/png"

	contentTypesPart = "/[Content_Types].xml"
	packageRelsPart  = "/_rels/.rels"
)

// Sentinel errors. Callers match them with errors.Is.
var (
	ErrInvalidPackage     = errors.New("invalid 3MF package")
	ErrLimitExceeded      = errors.New("3MF resource limit exceeded")
	ErrPartNotFound       = errors.New("part not found")
	ErrMalformedModel     = errors.New("malformed 3MF model")
	ErrUnsupportedRequire = errors.New("unsupported required extension")
	ErrInvalidReference   = errors.New("invalid object reference")
)

// Limits bound the work done on untrusted input. They are application policy,
// not limits defined by the 3MF specification.
type Limits struct {
	MaxEntries        int   // ZIP entries
	MaxPartSize       int64 // uncompressed bytes of one part
	MaxTotalRead      int64 // uncompressed bytes read across all parts
	MaxTriangles      int   // triangles parsed, and triangles produced by Flatten
	MaxComponentDepth int   // nesting depth while resolving components
}

// DefaultLimits returns generous limits suitable for desktop slicer projects.
func DefaultLimits() Limits {
	return Limits{
		MaxEntries:        10_000,
		MaxPartSize:       1 << 30,
		MaxTotalRead:      4 << 30,
		MaxTriangles:      50_000_000,
		MaxComponentDepth: 32,
	}
}

// Relationship is one OPC relationship with its target resolved to an
// absolute part name (unless External is true).
type Relationship struct {
	ID        string
	Type      string
	RawTarget string
	Target    string
	External  bool
}

// Entry is one ZIP entry. Directory entries have IsDir set and are copied
// through unchanged on save.
type Entry struct {
	Name  string // OPC part name ("/3D/3dmodel.model") or raw ZIP name for directories
	IsDir bool

	file     *zip.File
	data     []byte
	replaced []byte
}

// Package is an opened OPC/ZIP container. It keeps the original ZIP entries
// so unmodified payloads can be copied byte-for-byte when saving.
type Package struct {
	limits    Limits
	closer    io.Closer
	entries   []*Entry
	index     map[string]*Entry // lower-cased part name -> entry
	defaults  map[string]string // lower-cased extension -> content type
	overrides map[string]string // lower-cased part name -> content type
	rels      map[string][]Relationship
	readTotal int64
	comment   string // ZIP archive comment, kept on save
}

// OpenFile opens a 3MF file from disk. Close the package when done.
func OpenFile(name string, lim Limits) (*Package, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	p, err := Open(f, st.Size(), lim)
	if err != nil {
		f.Close()
		return nil, err
	}
	p.closer = f
	return p, nil
}

// OpenBytes opens an in-memory 3MF package.
func OpenBytes(b []byte, lim Limits) (*Package, error) {
	return Open(bytes.NewReader(b), int64(len(b)), lim)
}

// Open reads the ZIP directory, [Content_Types].xml and every .rels part.
// Model parts are not parsed here; see Load.
func Open(r io.ReaderAt, size int64, lim Limits) (*Package, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		// Includes zip.ErrInsecurePath ("../x", absolute names), which a
		// valid OPC package can never contain.
		return nil, fmt.Errorf("%w: %w", ErrInvalidPackage, err)
	}
	if len(zr.File) > lim.MaxEntries {
		return nil, fmt.Errorf("%w: %d ZIP entries (max %d)", ErrLimitExceeded, len(zr.File), lim.MaxEntries)
	}
	p := &Package{
		limits:    lim,
		index:     make(map[string]*Entry),
		defaults:  make(map[string]string),
		overrides: make(map[string]string),
		rels:      make(map[string][]Relationship),
		comment:   zr.Comment,
	}
	for _, f := range zr.File {
		e := &Entry{Name: f.Name, file: f}
		if strings.HasSuffix(f.Name, "/") {
			e.IsDir = true
			p.entries = append(p.entries, e)
			continue
		}
		name, err := partNameFromZip(f.Name)
		if err != nil {
			return nil, err
		}
		e.Name = name
		key := strings.ToLower(name)
		if _, dup := p.index[key]; dup {
			// OPC part names are compared case-insensitively.
			return nil, fmt.Errorf("%w: duplicate part name %q", ErrInvalidPackage, name)
		}
		p.index[key] = e
		p.entries = append(p.entries, e)
	}
	if err := p.loadContentTypes(); err != nil {
		return nil, err
	}
	if err := p.loadRelationships(); err != nil {
		return nil, err
	}
	return p, nil
}

// Close releases the underlying file when the package came from OpenFile.
func (p *Package) Close() error {
	if p.closer == nil {
		return nil
	}
	return p.closer.Close()
}

// Entries returns all ZIP entries in archive order.
func (p *Package) Entries() []*Entry { return p.entries }

// Has reports whether a part exists (case-insensitive).
func (p *Package) Has(part string) bool {
	_, ok := p.index[strings.ToLower(part)]
	return ok
}

// Read returns the uncompressed bytes of a part, honouring size limits and
// verifying the ZIP CRC.
func (p *Package) Read(part string) ([]byte, error) {
	e, ok := p.index[strings.ToLower(part)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPartNotFound, part)
	}
	if e.replaced != nil {
		return e.replaced, nil
	}
	if e.data != nil {
		return e.data, nil
	}
	if e.file.UncompressedSize64 > uint64(p.limits.MaxPartSize) {
		return nil, fmt.Errorf("%w: part %s declares %d bytes", ErrLimitExceeded, part, e.file.UncompressedSize64)
	}
	rc, err := e.file.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", part, err)
	}
	defer rc.Close()
	// The declared size can lie; bound the actual read as well.
	data, err := io.ReadAll(io.LimitReader(rc, p.limits.MaxPartSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", part, err) // includes zip.ErrChecksum
	}
	if int64(len(data)) > p.limits.MaxPartSize {
		return nil, fmt.Errorf("%w: part %s exceeds %d bytes", ErrLimitExceeded, part, p.limits.MaxPartSize)
	}
	p.readTotal += int64(len(data))
	if p.readTotal > p.limits.MaxTotalRead {
		return nil, fmt.Errorf("%w: more than %d uncompressed bytes read", ErrLimitExceeded, p.limits.MaxTotalRead)
	}
	e.data = data
	return data, nil
}

// ContentType resolves a part's content type: Override first, then Default
// by extension (both case-insensitive).
func (p *Package) ContentType(part string) string {
	if ct, ok := p.overrides[strings.ToLower(part)]; ok {
		return ct
	}
	ext := strings.TrimPrefix(path.Ext(part), ".")
	return p.defaults[strings.ToLower(ext)]
}

// Rels returns relationships whose source is part. Use "/" for the package.
func (p *Package) Rels(source string) []Relationship {
	return p.rels[strings.ToLower(source)]
}

// RelsOfType filters Rels(source) by relationship type.
func (p *Package) RelsOfType(source, relType string) []Relationship {
	var out []Relationship
	for _, r := range p.Rels(source) {
		if r.Type == relType {
			out = append(out, r)
		}
	}
	return out
}

// AllRels returns every relationship, keyed by lower-cased source part name.
func (p *Package) AllRels() map[string][]Relationship { return p.rels }

func partNameFromZip(zipName string) (string, error) {
	bad := func(why string) error {
		return fmt.Errorf("%w: invalid part name %q: %s", ErrInvalidPackage, zipName, why)
	}
	if zipName == "" || strings.HasPrefix(zipName, "/") {
		return "", bad("empty or absolute ZIP name")
	}
	if strings.Contains(zipName, `\`) {
		return "", bad("backslash")
	}
	for _, seg := range strings.Split(zipName, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", bad("empty or dot segment")
		}
	}
	return "/" + zipName, nil
}

func (p *Package) loadContentTypes() error {
	data, err := p.Read(contentTypesPart)
	if err != nil {
		return fmt.Errorf("%w: missing [Content_Types].xml: %w", ErrInvalidPackage, err)
	}
	var doc struct {
		XMLName  xml.Name `xml:"Types"`
		Defaults []struct {
			Extension   string `xml:"Extension,attr"`
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Default"`
		Overrides []struct {
			PartName    string `xml:"PartName,attr"`
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Override"`
	}
	if err := unmarshalStrict(data, &doc); err != nil {
		return fmt.Errorf("%w: [Content_Types].xml: %w", ErrInvalidPackage, err)
	}
	if doc.XMLName.Space != NSContentTypes {
		return fmt.Errorf("%w: [Content_Types].xml has namespace %q", ErrInvalidPackage, doc.XMLName.Space)
	}
	for _, d := range doc.Defaults {
		p.defaults[strings.ToLower(d.Extension)] = d.ContentType
	}
	for _, o := range doc.Overrides {
		p.overrides[strings.ToLower(o.PartName)] = o.ContentType
	}
	return nil
}

var relsPartRE = regexp.MustCompile(`^(.*/)_rels/([^/]*)\.rels$`)

func (p *Package) loadRelationships() error {
	for _, e := range p.entries {
		if e.IsDir {
			continue
		}
		m := relsPartRE.FindStringSubmatch(e.Name)
		if m == nil {
			continue
		}
		source := m[1] + m[2] // "/_rels/.rels" -> "/"
		data, err := p.Read(e.Name)
		if err != nil {
			return err
		}
		var doc struct {
			XMLName xml.Name `xml:"Relationships"`
			Rels    []struct {
				ID         string `xml:"Id,attr"`
				Type       string `xml:"Type,attr"`
				Target     string `xml:"Target,attr"`
				TargetMode string `xml:"TargetMode,attr"`
			} `xml:"Relationship"`
		}
		if err := unmarshalStrict(data, &doc); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrInvalidPackage, e.Name, err)
		}
		if doc.XMLName.Space != NSRelationships {
			return fmt.Errorf("%w: %s has namespace %q", ErrInvalidPackage, e.Name, doc.XMLName.Space)
		}
		for _, r := range doc.Rels {
			rel := Relationship{ID: r.ID, Type: r.Type, RawTarget: r.Target}
			if r.TargetMode == "External" {
				rel.External = true
				rel.Target = r.Target
			} else {
				t, err := resolveTarget(source, r.Target)
				if err != nil {
					return fmt.Errorf("%w: %s: %w", ErrInvalidPackage, e.Name, err)
				}
				rel.Target = t
			}
			key := strings.ToLower(source)
			p.rels[key] = append(p.rels[key], rel)
		}
	}
	return nil
}

// resolveTarget resolves a relationship target against its source part's
// folder, as OPC requires for relative references.
func resolveTarget(source, target string) (string, error) {
	u, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("target %q: %w", target, err)
	}
	if u.Scheme != "" || u.Host != "" {
		return "", fmt.Errorf("target %q is not a part reference", target)
	}
	t := u.Path
	if !strings.HasPrefix(t, "/") {
		t = path.Join(path.Dir(source), t)
	}
	return path.Clean("/" + strings.TrimPrefix(t, "/")), nil
}

// normalizePartRef turns a model-level path attribute (p:path, thumbnail)
// into a part name. Paths in model XML must be absolute (ST_UriReference).
func normalizePartRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "/") {
		return "", fmt.Errorf("%w: path %q is not absolute", ErrMalformedModel, ref)
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("%w: path %q: %w", ErrMalformedModel, ref, err)
	}
	return path.Clean(u.Path), nil
}

// unmarshalStrict rejects DTDs (Core 2.3.2 rule 2) before unmarshalling.
func unmarshalStrict(data []byte, v any) error {
	if err := rejectDTD(data); err != nil {
		return err
	}
	return xml.Unmarshal(data, v)
}

func rejectDTD(data []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.RawToken()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.Directive:
			if bytes.HasPrefix(bytes.TrimSpace(t), []byte("DOCTYPE")) {
				return errors.New("DTD content is not allowed")
			}
		case xml.StartElement:
			return nil // a DTD can only precede the root element
		}
	}
}
