package gitremote

import (
	"io"

	"golang.org/x/net/context"
)

type Command interface {
	setConfig(config Config)
	runCommand(r *runner, ctx context.Context) error
}

type CmdUnknown struct {
	Config Config
	Line   string
}

type CmdCapabilities struct {
	Config Config
}

type CmdList struct {
	Config  Config
	ForPush bool
}

type CmdOption struct {
	Config Config
	Key    string
	Value  string
}

type CmdFetch struct {
	Config  Config
	Objects map[string]string
}

type CmdPush struct {
	Config  Config
	Refs    []*PushRef
	Options []string
}

type CmdImport struct {
	Config Config
	Names  []string
	io.Reader
	io.Writer
}

type CmdExport struct {
	Config Config
	io.Reader
	io.Writer
}

type CmdConnect struct {
	Config  Config
	Service string
	io.Reader
	io.Writer
}

func (c *CmdUnknown) setConfig(config Config)      { c.Config = config }
func (c *CmdCapabilities) setConfig(config Config) { c.Config = config }
func (c *CmdList) setConfig(config Config)         { c.Config = config }
func (c *CmdOption) setConfig(config Config)       { c.Config = config }
func (c *CmdFetch) setConfig(config Config)        { c.Config = config }
func (c *CmdPush) setConfig(config Config)         { c.Config = config }
func (c *CmdImport) setConfig(config Config)       { c.Config = config }
func (c *CmdExport) setConfig(config Config)       { c.Config = config }
func (c *CmdConnect) setConfig(config Config)      { c.Config = config }

func (c *CmdUnknown) runCommand(r *runner, ctx context.Context) error {
	return r.Helper.Unknown(ctx, c)
}

func (c *CmdCapabilities) runCommand(r *runner, ctx context.Context) error {
	caps := r.Helper.Capabilities()
	return caps.writeTo(r.bw)
}

func (c *CmdList) runCommand(r *runner, ctx context.Context) error {
	refs, err := r.Helper.List(ctx, c)
	if err != nil {
		return err
	}

	l := listRefSlice(refs)
	return l.writeTo(r.bw)
}

func (c *CmdOption) runCommand(r *runner, ctx context.Context) error {
	err := r.Helper.SetOption(c.Key, c.Value)
	if err == ErrUnsupportedOption {
		_, err = r.bw.WriteString("unsupported\n")
		return err
	}
	if err != nil {
		_, err = r.bw.WriteString("error " + err.Error() + "\n")
		return err
	}
	_, err = r.bw.WriteString("ok\n")
	return err
}

func (c *CmdFetch) runCommand(r *runner, ctx context.Context) error {
	err := r.Helper.Fetch(ctx, c)
	if err != nil {
		return err
	}

	_, err = r.bw.WriteRune('\n')
	return err
}

func (c *CmdPush) runCommand(r *runner, ctx context.Context) error {
	err := r.Helper.Push(ctx, c)
	if err != nil {
		return err
	}

	s := pushRefSlice(c.Refs)
	return s.writeTo(r.bw)
}

func (c *CmdImport) runCommand(r *runner, ctx context.Context) error {
	return r.Helper.Import(ctx, c)
}

func (c *CmdExport) runCommand(r *runner, ctx context.Context) error {
	return r.Helper.Export(ctx, c)
}

func (c *CmdConnect) runCommand(r *runner, ctx context.Context) error {
	return r.Helper.Connect(ctx, c)
}
