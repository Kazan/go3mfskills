package threemf

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Save writes the package with pending edits applied.
//
// Preservation contract (proposed Go policy, not a 3MF requirement):
//   - Entries without edits are copied with zip.Writer.Copy: the compressed
//     bytes, CRC, method and names are reused, so their payload is
//     bit-identical. This includes parts the library does not understand
//     (Bambu Metadata/*, thumbnails, G-code, custom parts) whether or not
//     they carry a MustPreserve relationship.
//   - Edited model parts keep every byte outside the edited elements.
//   - Entry order and the archive comment are kept. The ZIP container itself
//     (local header extras, timestamps of rewritten entries, data
//     descriptors) is not guaranteed to be byte-identical.
func (d *Document) Save(w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, e := range d.Pkg.Entries() {
		data := []byte(nil)
		if !e.IsDir {
			data = d.render(e.Name)
			if data == nil {
				data = e.replaced
			}
		}
		if data == nil {
			if err := zw.Copy(e.file); err != nil {
				return fmt.Errorf("copy %s: %w", e.Name, err)
			}
			continue
		}
		fh := e.file.FileHeader
		hdr := &zip.FileHeader{
			Name:           fh.Name,
			Comment:        fh.Comment,
			Method:         fh.Method,
			Modified:       fh.Modified,
			ExternalAttrs:  fh.ExternalAttrs,
			CreatorVersion: fh.CreatorVersion,
		}
		if hdr.Method != zip.Store {
			hdr.Method = zip.Deflate
		}
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			return fmt.Errorf("create %s: %w", e.Name, err)
		}
		if _, err := fw.Write(data); err != nil {
			return fmt.Errorf("write %s: %w", e.Name, err)
		}
	}
	if d.Pkg.comment != "" {
		if err := zw.SetComment(d.Pkg.comment); err != nil {
			return err
		}
	}
	return zw.Close()
}

// SaveFile writes to a temporary file in the destination directory, syncs
// it and renames it over name, so a failure never leaves a partial 3MF.
// On Unix, saving over the file the package was opened from works because
// the open source file stays readable after the rename; on Windows the
// rename fails while the source is open, so save to a new name there.
func (d *Document) SaveFile(name string) (err error) {
	f, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err = d.Save(f); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, name)
}

// ReplacePart sets new bytes for an existing non-model part (for example a
// regenerated thumbnail). Model parts must be changed through the edit API so
// the in-memory model and the bytes stay consistent. Adding or deleting parts
// is out of scope for this example: it also requires updating
// [Content_Types].xml and relationships.
func (p *Package) ReplacePart(part string, data []byte) error {
	e, ok := p.index[strings.ToLower(part)]
	if !ok {
		return fmt.Errorf("%w: %s", ErrPartNotFound, part)
	}
	if p.ContentType(part) == ContentTypeModel {
		return errors.New("threemf: use the Document edit API for model parts")
	}
	if data == nil {
		data = []byte{}
	}
	e.replaced = data
	return nil
}
