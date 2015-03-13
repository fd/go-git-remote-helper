package peernet

import (
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/inconshreveable/muxado"
	"golang.org/x/net/websocket"
)

type Peer struct {
	session muxado.Session
	handler http.Handler
	client  *http.Client
}

func Dial(url string, handler http.Handler) (*Peer, error) {
	ws, err := websocket.Dial(url, "", "http://localhost/")
	if err != nil {
		return nil, err
	}

	sess := muxado.Client(ws)

	client := &http.Client{
		Transport: &http.Transport{
			Dial:              sess.NetDial,
			DialTLS:           sess.NetDial,
			DisableKeepAlives: true,
		},
	}

	if handler == nil {
		handler = http.HandlerFunc(http.NotFound)
	}

	peer := &Peer{sess, handler, client}

	go http.Serve(sess.NetListener(), peerHandler(peer))

	return peer, nil
}

func Listen(addr string, handler http.Handler) error {
	if handler == nil {
		handler = http.HandlerFunc(http.NotFound)
	}

	var h = func(ws *websocket.Conn) {
		sess := muxado.Server(ws)

		client := &http.Client{
			Transport: &http.Transport{
				Dial:              sess.NetDial,
				DialTLS:           sess.NetDial,
				DisableKeepAlives: true,
			},
		}

		peer := &Peer{sess, handler, client}

		defer peer.session.Close()

		http.Serve(peer.session.NetListener(), peerHandler(peer))
	}

	return http.ListenAndServe(addr, websocket.Handler(h))
}

func (p *Peer) Do(req *http.Request) (resp *http.Response, err error) {
	if req != nil {
		req.URL.Scheme = "http"
		req.URL.Host = "_"
	}
	return p.client.Do(req)
}

func (p *Peer) Get(url string) (resp *http.Response, err error) {
	return p.client.Get("http://_" + url)
}

func (p *Peer) Head(url string) (resp *http.Response, err error) {
	return p.client.Head("http://_" + url)
}

func (p *Peer) Post(url string, bodyType string, body io.Reader) (resp *http.Response, err error) {
	return p.client.Post("http://_"+url, bodyType, body)
}

func (p *Peer) PostForm(url string, data url.Values) (resp *http.Response, err error) {
	return p.client.PostForm("http://_"+url, data)
}

func (p *Peer) Close() error {
	return p.session.Close()
}

var (
	peersMtx sync.RWMutex
	peers    = map[*http.Request]*Peer{}
)

func peerHandler(p *Peer) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		peersMtx.Lock()
		peers[req] = p
		peersMtx.Unlock()

		defer func() {
			peersMtx.Lock()
			delete(peers, req)
			peersMtx.Unlock()
		}()

		p.handler.ServeHTTP(rw, req)
	}
}

func LookupPeer(req *http.Request) *Peer {
	peersMtx.RLock()
	defer peersMtx.RUnlock()

	return peers[req]
}
