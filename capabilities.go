package gitremote

import (
	"bytes"
	"io"
)

type Capabilities struct {
	Optional    Capability
	Mandatory   Capability
	Refspecs    []string
	ExportMarks string
	ImportMarks string
}

type Capability uint

const (
	CapConnect Capability = 1 << iota
	CapPush
	CapFetch
	CapExport
	CapImport
	CapOption
	CapRefspec
	CapBidiImport
	CapExportMarks
	CapImportMarks
	CapNoPrivateUpdate
	CapCheckConnectivity
	CapSignedTags
)

func (c Capability) String() string {
	switch c {
	case CapConnect:
		return "connect"
	case CapPush:
		return "push"
	case CapFetch:
		return "fetch"
	case CapExport:
		return "export"
	case CapImport:
		return "import"
	case CapOption:
		return "option"
	case CapRefspec:
		return "refspec"
	case CapBidiImport:
		return "bidi-import"
	case CapExportMarks:
		return "export-marks"
	case CapImportMarks:
		return "import-marks"
	case CapNoPrivateUpdate:
		return "no-private-update"
	case CapCheckConnectivity:
		return "check-connectivity"
	case CapSignedTags:
		return "signed-tags"
	default:
		panic("unknown Capability")
	}
}

func (c Capabilities) writeTo(w io.Writer) error {
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

	callExtra := func(extra func()) {
		if err != nil {
			return
		}
		if extra == nil {
			return
		}

		extra()
	}

	writeCap := func(cap Capability, extra func()) {
		if err != nil {
			return
		}

		if c.Mandatory&cap == cap {
			writeRune('*')
			writeString(cap.String())
			callExtra(extra)
			writeRune('\n')
			return
		}

		if c.Optional&cap == cap {
			writeString(cap.String())
			callExtra(extra)
			writeRune('\n')
			return
		}
	}

	writeCap(CapConnect, nil)
	writeCap(CapPush, nil)
	writeCap(CapFetch, nil)
	writeCap(CapExport, nil)
	writeCap(CapImport, nil)
	writeCap(CapOption, nil)
	writeCap(CapBidiImport, nil)

	writeCap(CapExportMarks, func() {
		writeRune(' ')
		writeString(c.ExportMarks)
	})

	writeCap(CapImportMarks, func() {
		writeRune(' ')
		writeString(c.ImportMarks)
	})

	for _, refspec := range c.Refspecs {
		writeCap(CapRefspec, func() {
			writeRune(' ')
			writeString(refspec)
		})
	}

	writeRune('\n')

	if err == nil {
		_, err = buf.WriteTo(w)
	}

	return err
}
