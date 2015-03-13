package gitremote

import (
	"bytes"
	"io"
)

type ListRef struct {
	Name string

	Hash string // sha1 hash  value
	Sym  string // @<dest> for symref
	// When Hash and Sym are blank the <value> is '?'

	Unchanged bool // unchanged attribute
}

type PushRef struct {
	Src   string
	Dst   string
	Force bool

	Ok  bool
	Err error
}

type listRefSlice []ListRef

func (r listRefSlice) writeTo(w io.Writer) error {
	var buf bytes.Buffer
	var err error

	writeRune := func(r rune) {
		if err != nil {
			return
		}

		_, err = buf.WriteRune(r)
	}

	writeString := func(s string) {
		if err != nil {
			return
		}

		_, err = buf.WriteString(s)
	}

	writeRef := func(ref ListRef) {
		if err != nil {
			return
		}

		if ref.Hash != "" {
			writeString(ref.Hash)
		} else if ref.Sym != "" {
			writeRune('@')
			writeString(ref.Sym)
		} else {
			writeRune('?')
		}

		writeRune(' ')
		writeString(ref.Name)

		if ref.Unchanged {
			writeRune(' ')
			writeString("unchanged")
		}

		writeRune('\n')
	}

	for _, ref := range r {
		writeRef(ref)
	}

	writeRune('\n')

	if err == nil {
		_, err = buf.WriteTo(w)
	}

	return err
}

type pushRefSlice []*PushRef

func (r pushRefSlice) writeTo(w io.Writer) error {
	var buf bytes.Buffer
	var err error

	writeRune := func(r rune) {
		if err != nil {
			return
		}

		_, err = buf.WriteRune(r)
	}

	writeString := func(s string) {
		if err != nil {
			return
		}

		_, err = buf.WriteString(s)
	}

	writeRef := func(ref *PushRef) {
		if err != nil {
			return
		}

		if ref.Ok {
			writeString("ok ")
			writeString(ref.Dst)
		} else {
			writeString("error ")
			writeString(ref.Dst)
			if ref.Err != nil {
				writeRune(' ')
				writeString(ref.Err.Error())
			}
		}

		writeRune('\n')
	}

	for _, ref := range r {
		writeRef(ref)
	}

	writeRune('\n')

	if err == nil {
		_, err = buf.WriteTo(w)
	}

	return err
}
