package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/mux"

	"bitbucket.org/simonmenke/featherhead/pkg/git/objects"
	"github.com/fd/go-git-remote-helper/example/peernet"
)

func main() {
	r := mux.NewRouter()

	r.HandleFunc("/{repo}/refs", handleRefs).Methods("GET")
	r.HandleFunc("/{repo}/refs", handlePush).Methods("POST")
	r.HandleFunc("/objects/{hash}", handleObject).Methods("GET")

	peernet.Listen(":3000", r)
}

func handleRefs(rw http.ResponseWriter, req *http.Request) {
	var (
		vars = mux.Vars(req)
		repo = vars["repo"]
		refs map[string]string
	)

	mtx.RLock()
	defer mtx.RUnlock()

	refs, ok := allRefs[repo]
	if !ok {
		http.NotFound(rw, req)
		return
	}
	if refs == nil {
		refs = map[string]string{}
	}

	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(200)
	json.NewEncoder(rw).Encode(refs)
}

func handlePush(rw http.ResponseWriter, req *http.Request) {
	var (
		vars = mux.Vars(req)
		peer = peernet.LookupPeer(req)
		repo = vars["repo"]
		refs map[string]string
	)

	mtx.Lock()
	defer mtx.Unlock()

	refs, ok := allRefs[repo]
	if !ok {
		http.NotFound(rw, req)
		return
	}

	var refOps []*struct {
		Name  string
		Hash  string
		Force bool

		Ok  bool
		Err string
	}

	err := json.NewDecoder(req.Body).Decode(&refOps)
	if err != nil {
		panic(err)
	}

	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(200)

	for _, op := range refOps {

		// delete ref
		if op.Hash == "" {
			if _, f := refs[op.Name]; f {
				delete(refs, op.Name)
				op.Ok = true
			} else {
				op.Ok = false
				op.Err = "not found"
			}
			continue
		}

		// update ref
		prevHash := refs[op.Name]
		foundPrevHash := false
		err := <-loadObject(peer, op.Hash, func(hash string) error {
			if prevHash == hash {
				foundPrevHash = true
			}
			return nil
		})
		if err != nil {
			log.Printf("error: %s", err)
			op.Ok = false
			op.Err = fmt.Sprintf("failed to load all objects")
			continue
		}

		if prevHash == "" || foundPrevHash {
			refs[op.Name] = op.Hash
			op.Ok = true
		} else {
			op.Ok = false
			op.Err = "not fast-forward"
		}
	}

	json.NewEncoder(rw).Encode(refOps)
}

func handleObject(rw http.ResponseWriter, req *http.Request) {
	var (
		vars = mux.Vars(req)
		hash = vars["hash"]
	)

	mtx.RLock()
	o, found := objectMap[hash]
	mtx.RUnlock()

	if !found {
		http.NotFound(rw, req)
		return
	}

	rw.WriteHeader(200)
	rw.Write(o)
}

var (
	mtx     sync.RWMutex
	allRefs = map[string]map[string]string{
		"bootloader": {},
	}
	objectMap = map[string][]byte{}
)

type beforeLoadFunc func(hash string) error

func loadObject(peer *peernet.Peer, hash string, f beforeLoadFunc) <-chan error {
	out := make(chan error, 1)
	go func() {
		defer close(out)

		err := f(hash)
		if err != nil {
			out <- err
			return
		}

		if _, found := objectMap[hash]; found {
			return
		}

		resp, err := peer.Get("/objects/" + hash)
		if err != nil {
			out <- err
			return
		}

		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			out <- fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			out <- err
			return
		}

		objectMap[hash] = data
		var q []<-chan error

		r, err := objects.NewReader(bytes.NewReader(data))
		if err != nil {
			out <- err
			return
		}

		switch r.Type() {

		case objects.CommitType:
			o, err := objects.Parse(r)
			if err != nil {
				out <- err
				return
			}
			c := o.(*objects.Commit)
			q = append(q, loadObject(peer, c.Tree, f))
			for _, parent := range c.Parents {
				q = append(q, loadObject(peer, parent, f))
			}

		case objects.TreeType:
			o, err := objects.Parse(r)
			if err != nil {
				out <- err
				return
			}
			t := o.(*objects.Tree)
			for _, e := range t.Entries {
				q = append(q, loadObject(peer, e.Sha, f))
			}

		}

		// wait for others
		for _, p := range q {
			err := <-p
			if err != nil {
				out <- err
				return
			}
		}

		log.Printf("Loaded: %q", hash)
	}()
	return out
}
