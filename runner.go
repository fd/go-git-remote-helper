package gitremote

import (
	"bufio"
	"errors"
	"io"
	"os"
	"sync"

	"golang.org/x/net/context"
)

var ErrInvalidArguments = errors.New("invalid arguments.")
var ErrUnsupportedOption = errors.New("unsupported option")

type Helper interface {
	Capabilities() Capabilities
	SetOption(key, value string) error

	List(ctx context.Context, cmd *CmdList) ([]ListRef, error)
	Fetch(ctx context.Context, cmd *CmdFetch) error
	Push(ctx context.Context, cmd *CmdPush) error
	Export(ctx context.Context, cmd *CmdExport) error
	Import(ctx context.Context, cmd *CmdImport) error
	Connect(ctx context.Context, cmd *CmdConnect) error
	Unknown(ctx context.Context, cmd *CmdUnknown) error
}

type Config struct {
	Helper Helper
	Dir    string
	Remote string
	URL    string
	Stdin  io.Reader
	Stdout io.Writer
	Err    error
}

type runner struct {
	Config
	mtx sync.Mutex
	br  *bufio.Reader
	bw  *bufio.Writer
	err error
}

func DefaultConfig() Config {
	c := Config{}

	args := os.Args[1:]
	if len(args) == 0 || len(args) > 2 {
		c.Err = ErrInvalidArguments
		return c
	}

	c.Dir = os.Getenv("GIT_DIR")
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Remote = args[0]

	c.URL = args[0]
	if len(args) > 1 {
		c.URL = args[1]
	}

	return c
}

func Run(ctx context.Context, config Config) error {
	if config.Err != nil {
		return config.Err
	}

	var r runner
	r.Config = config

	return r.run(ctx)
}

func (r *runner) run(ctx context.Context) error {
	r.mtx.Lock()
	r.br = bufio.NewReader(r.Stdin)
	r.bw = bufio.NewWriter(r.Stdout)
	r.mtx.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	commands := r.readCommands(ctx)

LOOP:
	for {
		select {

		case <-ctx.Done():
			if r.setError(ctx.Err()) {
				break LOOP
			}

		case cmd, ok := <-commands:
			if !ok {
				break LOOP
			}
			if r.setError(r.runCommand(ctx, cmd)) {
				break LOOP
			}

		}
	}

	return r.err
}

func (r *runner) runCommand(ctx context.Context, cmd Command) error {
	cmd.setConfig(r.Config)

	err := cmd.runCommand(r, ctx)

	flushErr := r.bw.Flush()
	if err == nil {
		err = flushErr
	}

	return err
}

func (r *runner) setError(err error) bool {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	if r.err == nil {
		r.err = err
	}

	return r.err != nil
}
