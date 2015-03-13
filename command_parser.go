package gitremote

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/context"
)

type ErrInvalidCommand string

func (e ErrInvalidCommand) Error() string {
	return fmt.Sprintf("invalid command: %q", string(e))
}

func (r *runner) readCommands(ctx context.Context) <-chan Command {
	var out = make(chan Command)

	go func() {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		defer close(out)

		for {
			cmd, err := r.readCommand()
			if err == io.EOF {
				return
			}
			if r.setError(err) {
				return
			}

			select {
			case <-ctx.Done():
				return
			case out <- cmd:
			}
		}
	}()

	return out
}

func (r *runner) readCommand() (Command, error) {
	var (
		cmd       Command
		fetchCmd  *CmdFetch
		pushCmd   *CmdPush
		importCmd *CmdImport
		state     int
	)

MORE:
	str, err := r.br.ReadString('\n')
	if err == io.EOF {
		return nil, io.ErrUnexpectedEOF
	}
	if err != nil {
		return nil, err
	}

	str = strings.TrimSuffix(str, "\n")

	switch state {

	case 0: // root
		switch {
		case str == "":
		// done

		case str == "capabilities":
			cmd = &CmdCapabilities{}

		case str == "list":
			cmd = &CmdList{}

		case str == "list for-push":
			cmd = &CmdList{ForPush: true}

		case strings.HasPrefix(str, "option "):
			parts := strings.SplitN(str, " ", 3)
			if len(parts) != 3 {
				cmd = &CmdUnknown{Line: str}
			} else {
				cmd = &CmdOption{Key: parts[1], Value: parts[2]}
			}

		case strings.HasPrefix(str, "fetch "):
			parts := strings.SplitN(str, " ", 3)
			if len(parts) != 3 {
				cmd = &CmdUnknown{Line: str}
			} else {
				fetchCmd = &CmdFetch{Objects: map[string]string{parts[1]: parts[2]}}
				cmd = fetchCmd
				state = 1
				goto MORE
			}

		case strings.HasPrefix(str, "push "):
			parts := strings.SplitN(str, " ", 2)
			if len(parts) != 2 {
				cmd = &CmdUnknown{Line: str}
			} else {
				var ref = &PushRef{}

				if strings.HasPrefix(parts[1], "+") {
					ref.Force = true
					parts[1] = parts[1][1:]
				}

				parts = strings.Split(parts[1], ":")
				if len(parts) != 2 {
					cmd = &CmdUnknown{Line: str}
				} else {
					ref.Src = parts[0]
					ref.Dst = parts[1]

					pushCmd = &CmdPush{Refs: []*PushRef{ref}}
					cmd = pushCmd
					state = 2
					goto MORE
				}
			}

		case strings.HasPrefix(str, "import "):
			parts := strings.SplitN(str, " ", 2)
			if len(parts) != 2 {
				cmd = &CmdUnknown{Line: str}
			} else {
				importCmd = &CmdImport{Names: []string{parts[1]}, Reader: r.br, Writer: r.bw}
				cmd = importCmd
				state = 3
				goto MORE
			}

		case str == "export":
			cmd = &CmdExport{Reader: r.br, Writer: r.bw}

		case strings.HasPrefix(str, "connect "):
			parts := strings.SplitN(str, " ", 2)
			if len(parts) != 2 {
				cmd = &CmdUnknown{Line: str}
			} else {
				cmd = &CmdConnect{Service: parts[1], Reader: r.br, Writer: r.bw}
			}

		default:
			cmd = &CmdUnknown{Line: str}

		}

	case 1: // fetch
		switch {
		case str == "":
			// done

		case strings.HasPrefix(str, "fetch "):
			parts := strings.SplitN(str, " ", 3)
			if len(parts) != 3 {
				return nil, ErrInvalidCommand(str)
			} else {
				fetchCmd.Objects[parts[1]] = parts[2]
				goto MORE
			}

		default:
			return nil, ErrInvalidCommand(str)

		}

	case 2: // fetch
		switch {
		case str == "":
			// done

		case strings.HasPrefix(str, "push "):
			parts := strings.SplitN(str, " ", 2)
			if len(parts) != 2 {
				return nil, ErrInvalidCommand(str)
			} else {
				var ref = &PushRef{}

				if strings.HasPrefix(parts[1], "+") {
					ref.Force = true
					parts[1] = parts[1][1:]
				}

				parts = strings.Split(parts[1], ":")
				if len(parts) != 2 {
					return nil, ErrInvalidCommand(str)
				} else {
					ref.Src = parts[0]
					ref.Dst = parts[1]

					pushCmd.Refs = append(pushCmd.Refs, ref)
					goto MORE
				}
			}

		default:
			pushCmd.Options = append(pushCmd.Options, str)
			goto MORE

		}

	case 3: // import
		switch {
		case str == "":
			// done

		case strings.HasPrefix(str, "import "):
			parts := strings.SplitN(str, " ", 2)
			if len(parts) != 2 {
				return nil, ErrInvalidCommand(str)
			} else {
				importCmd.Names = append(importCmd.Names, parts[1])
				goto MORE
			}

		default:
			return nil, ErrInvalidCommand(str)

		}

	}

	if cmd == nil {
		return nil, io.EOF
	}

	return cmd, nil
}
