package main

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/fd/git"
	"github.com/gorilla/mux"
	"golang.org/x/net/context"

	"github.com/fd/go-git-remote-helper"
	"github.com/fd/go-git-remote-helper/example/peernet"
)

func main() {
	conf := gitremote.DefaultConfig()

	fmt.Fprintf(os.Stderr, "config: %v\n", conf)

	u, err := url.Parse(conf.URL)
	assert(err)

	repo, err := git.OpenRepository(conf.Dir)
	assert(err)

	repoName := strings.TrimPrefix(strings.TrimSuffix(u.Path, ".git"), "/")
	u.Path = "/"
	u.Scheme = "ws"

	r := mux.NewRouter()
	r.HandleFunc("/objects/{hash}", objectHandler(repo)).Methods("GET")

	peer, err := peernet.Dial(u.String(), r)
	assert(err)

	conf.Helper = &Helper{peer: peer, repoName: repoName, repo: repo}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = gitremote.Run(ctx, conf)
	assert(err)
}

func assert(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

type Helper struct {
	peer     *peernet.Peer
	repoName string
	repo     *git.Repository

	mtx         sync.Mutex
	loaderCache map[string]bool
}

func (h *Helper) Capabilities() gitremote.Capabilities {
	cap := gitremote.Capabilities{}
	cap.Mandatory = gitremote.CapPush | gitremote.CapFetch
	cap.Optional = gitremote.CapOption
	return cap
}

func (h *Helper) SetOption(key, value string) error {
	fmt.Fprintf(os.Stderr, "option %q %q\n", key, value)
	return nil
}

func (h *Helper) List(ctx context.Context, cmd *gitremote.CmdList) ([]gitremote.ListRef, error) {
	resp, err := h.peer.Get("/" + h.repoName + "/refs")
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code %d for %q", resp.StatusCode, resp.Request.URL)
	}

	var body map[string]string
	err = json.NewDecoder(resp.Body).Decode(&body)
	if err != nil {
		return nil, err
	}

	var (
		refs      []gitremote.ListRef
		hasMaster bool
		hasHead   bool
	)

	for name, hash := range body {
		if name == "HEAD" {
			hasHead = true
		}
		if name == "refs/heads/master" {
			hasMaster = true
		}

		refs = append(refs, gitremote.ListRef{
			Name: name,
			Hash: hash,
		})
	}

	if hasMaster && !hasHead {
		refs = append(refs, gitremote.ListRef{
			Name: "HEAD",
			Sym:  "refs/heads/master",
		})
	}

	return refs, nil
}

func (h *Helper) Fetch(ctx context.Context, cmd *gitremote.CmdFetch) error {
	for hash, _ := range cmd.Objects {
		err := <-h.loadObject(hash)
		if err != nil {
			return err
		}
	}

	return nil
}

func (h *Helper) Push(ctx context.Context, cmd *gitremote.CmdPush) error {
	type RefOp struct {
		Name  string
		Hash  string
		Force bool

		Ok  bool
		Err string
	}

	var (
		ops []RefOp
		buf bytes.Buffer
	)

	for _, ref := range cmd.Refs {
		var (
			hash string
			err  error
		)

		if ref.Src != "" {
			hash, err = h.repo.GetCommitIdOfRef(ref.Src)
			if err != nil {
				return err
			}
		}

		ops = append(ops, RefOp{Name: ref.Dst, Hash: hash, Force: ref.Force})
	}

	err := json.NewEncoder(&buf).Encode(ops)
	if err != nil {
		return err
	}

	resp, err := h.peer.Post("/"+h.repoName+"/refs", "application/json; charset=utf-8", &buf)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status code %d for %q", resp.StatusCode, resp.Request.URL)
	}

	ops = nil
	err = json.NewDecoder(resp.Body).Decode(&ops)
	if err != nil {
		return err
	}

	for _, op := range ops {
		for _, ref := range cmd.Refs {
			if ref.Dst == op.Name {
				ref.Ok = op.Ok

				if op.Err != "" {
					ref.Err = fmt.Errorf(op.Err)
				}

				break
			}
		}
	}

	return nil
}

func (h *Helper) Export(ctx context.Context, cmd *gitremote.CmdExport) error {
	panic("unsupported command: Export")
}

func (h *Helper) Import(ctx context.Context, cmd *gitremote.CmdImport) error {
	panic("unsupported command: Import")
}

func (h *Helper) Connect(ctx context.Context, cmd *gitremote.CmdConnect) error {
	panic("unsupported command: Connect")
}

func (h *Helper) Unknown(ctx context.Context, cmd *gitremote.CmdUnknown) error {
	panic(fmt.Sprintf("unsupported command: %q", cmd.Line))
}

func objectHandler(repo *git.Repository) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		var (
			vars = mux.Vars(req)
			hash = vars["hash"]
			err  error
		)

		typ, length, rc, err := repo.GetRawObject(hash, false)
		if git.IsObjectNotFound(err) {
			http.NotFound(rw, req)
			return
		}
		if err != nil {
			panic(err)
		}

		defer rc.Close()

		fmt.Fprintf(os.Stderr, "sending %s %q %d\n", typ, hash, length)

		header := fmt.Sprintf("%s %d\x00", strings.ToLower(typ.String()), length)

		rw.Header().Set("Content-Length", fmt.Sprintf("%d", int64(len(header))+length))
		rw.WriteHeader(200)

		_, err = io.WriteString(rw, header)
		if err != nil {
			panic(err)
		}

		_, err = io.Copy(rw, rc)
		if err != nil {
			panic(err)
		}
	}
}

func (h *Helper) loadObject(hash string) <-chan error {
	out := make(chan error, 1)
	go func() {
		defer close(out)

		if !h.needsObject(hash) {
			return
		}

		_, _, _, err := h.repo.GetRawObject(hash, true)
		if err == nil {
			return
		}
		if git.IsObjectNotFound(err) {
			err = nil
		}
		if err != nil {
			out <- err
			return
		}

		resp, err := h.peer.Get("/objects/" + hash)
		if err != nil {
			out <- err
			return
		}

		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			out <- fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		dir := path.Join(os.Getenv("GIT_DIR"), "objects", hash[:2])
		fname := path.Join(dir, hash[2:])

		err = os.MkdirAll(dir, 0755)
		if err != nil {
			out <- err
			return
		}

		f, err := os.Create(fname)
		if err != nil {
			out <- err
			return
		}

		defer f.Close()

		w := zlib.NewWriter(f)
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "HERE H: %s\n", err)
			out <- err
			return
		}

		err = w.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "HERE G: %s\n", err)
			out <- err
			return
		}

		err = f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "HERE F: %s\n", err)
			out <- err
			return
		}

		var q []<-chan error

		typ, _, _, err := h.repo.GetRawObject(hash, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "HERE E: %s (%s)\n", err, typ)
			out <- err
			return
		}

		switch typ {

		case git.ObjectCommit:
			c, err := h.repo.GetCommit(hash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "HERE D: %s\n", err)
				out <- err
				return
			}

			q = append(q, h.loadObject(c.TreeId().String()))
			for i, l := 0, c.ParentCount(); i < l; i++ {
				id, err := c.ParentId(i)
				if err != nil {
					fmt.Fprintf(os.Stderr, "HERE C: %s\n", err)
					out <- err
					return
				}

				q = append(q, h.loadObject(id.String()))
			}

		case git.ObjectTree:
			t, err := h.repo.GetTree(hash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "HERE B: %s\n", err)
				out <- err
				return
			}

			for _, e := range t.ListEntries() {
				q = append(q, h.loadObject(e.Id.String()))
			}

		case git.ObjectTag:
			t, err := h.repo.GetTagWithId(hash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "HERE A: %s\n", err)
				out <- err
				return
			}

			q = append(q, h.loadObject(t.Object.String()))

		}

		// wait for others
		for _, p := range q {
			err := <-p
			if err != nil {
				out <- err
				return
			}
		}

		fmt.Fprintf(os.Stderr, "Loaded: %q\n", hash)
	}()
	return out
}

func (h *Helper) needsObject(hash string) bool {
	h.mtx.Lock()
	defer h.mtx.Unlock()

	if h.loaderCache == nil {
		h.loaderCache = make(map[string]bool)
	}

	if h.loaderCache[hash] {
		return false
	}

	h.loaderCache[hash] = true
	return true
}
