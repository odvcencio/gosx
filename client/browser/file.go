package browser

import "errors"

// ErrNoFile reports that the viewer closed the file picker without choosing
// a file.
var ErrNoFile = errors.New("browser: no file chosen")

// ErrFileTooLarge reports that the chosen file is larger than
// FileRequest.MaxBytes. OpenFile does not read such a file.
var ErrFileTooLarge = errors.New("browser: file too large")

// DefaultFileMaxBytes is the largest file OpenFile reads when
// FileRequest.MaxBytes is zero: 64 MiB.
const DefaultFileMaxBytes = 64 << 20

// File is a file the viewer chose, read whole.
type File struct {
	// Name is the file's name without its folder, as the browser reports it.
	Name string
	// Type is the MIME type the browser guessed from the name; it may be
	// empty.
	Type string
	Data []byte
}

// FileRequest describes the file picker that OpenFile shows.
type FileRequest struct {
	// Accept lists the file types the picker offers, as in the accept
	// attribute of an input of type file: ".json,application/json". Empty
	// offers every file. The viewer can still choose another type.
	Accept string
	// MaxBytes refuses a larger file with ErrFileTooLarge. Zero means
	// DefaultFileMaxBytes.
	MaxBytes int
}

func (r FileRequest) maxBytes() int {
	if r.MaxBytes <= 0 {
		return DefaultFileMaxBytes
	}
	return r.MaxBytes
}

// saveFileType is the MIME type SaveFile uses when the caller gives none.
const saveFileType = "application/octet-stream"
