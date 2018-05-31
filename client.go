package stdsdk

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	sortableTime = "20060102.150405.000000000"
)

type Client struct {
	Endpoint *url.URL
	Prepare  PrepareFunc
}

type PrepareFunc func(req *http.Request)

var DefaultClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 10 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
}

func New(endpoint string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	return &Client{Endpoint: u}, nil
}

func (c *Client) Head(path string, opts RequestOptions, out *bool) error {
	req, err := c.Request("HEAD", path, opts)
	if err != nil {
		return err
	}

	res, err := c.handleRequest(req)
	if err != nil {
		return err
	}

	switch res.StatusCode / 100 {
	case 2:
		*out = true
	default:
		*out = false
	}

	return nil
}

func (c *Client) Options(path string, opts RequestOptions, out interface{}) error {
	req, err := c.Request("OPTIONS", path, opts)
	if err != nil {
		return err
	}

	res, err := c.handleRequest(req)
	if err != nil {
		return err
	}

	return unmarshalReader(res.Body, out)
}

func (c *Client) GetStream(path string, opts RequestOptions) (*http.Response, error) {
	req, err := c.Request("GET", path, opts)
	if err != nil {
		return nil, err
	}

	return c.handleRequest(req)
}

func (c *Client) Get(path string, opts RequestOptions, out interface{}) error {
	res, err := c.GetStream(path, opts)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	return unmarshalReader(res.Body, out)
}

func (c *Client) PostStream(path string, opts RequestOptions) (*http.Response, error) {
	req, err := c.Request("POST", path, opts)
	if err != nil {
		return nil, err
	}

	res, err := c.handleRequest(req)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (c *Client) Post(path string, opts RequestOptions, out interface{}) error {
	res, err := c.PostStream(path, opts)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	return unmarshalReader(res.Body, out)
}

func (c *Client) PutStream(path string, opts RequestOptions) (*http.Response, error) {
	req, err := c.Request("PUT", path, opts)
	if err != nil {
		return nil, err
	}

	return c.handleRequest(req)
}

func (c *Client) Put(path string, opts RequestOptions, out interface{}) error {
	res, err := c.PutStream(path, opts)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	return unmarshalReader(res.Body, out)
}

func (c *Client) Delete(path string, opts RequestOptions, out interface{}) error {
	req, err := c.Request("DELETE", path, opts)
	if err != nil {
		return err
	}

	res, err := c.handleRequest(req)
	if err != nil {
		return err
	}

	return unmarshalReader(res.Body, out)
}

func (c *Client) Websocket(path string, opts RequestOptions) (io.ReadCloser, error) {
	var u url.URL

	u = *c.Endpoint

	u.Scheme = "wss"
	u.Path += path
	u.User = nil

	h := http.Header{}

	for k, v := range opts.Headers {
		h.Add(k, v)
	}

	if c.Endpoint.User != nil {
		h.Add("Authorization", fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s", c.Endpoint.User)))))
	}

	websocket.DefaultDialer.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	ws, _, err := websocket.DefaultDialer.Dial(u.String(), h)
	if err != nil {
		return nil, err
	}

	r, w := io.Pipe()

	or, err := opts.Reader()
	if err != nil {
		return nil, err
	}

	go websocketIn(ws, or)
	go websocketOut(w, ws)

	return r, nil
}

func websocketIn(ws *websocket.Conn, r io.Reader) {
	buf := make([]byte, 10*1024)

	for {
		n, err := r.Read(buf)
		switch err {
		case io.EOF:
			ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""))
		case nil:
			ws.WriteMessage(websocket.TextMessage, buf[0:n])
		default:
			ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()))
			return
		}
	}
}

func websocketOut(w io.WriteCloser, ws *websocket.Conn) {
	defer w.Close()

	for {
		code, data, err := ws.ReadMessage()
		switch err {
		case io.EOF:
			return
		case nil:
			switch code {
			case websocket.TextMessage:
				w.Write(data)
			}
		default:
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				fmt.Fprintf(w, "ERROR: %s\n", err.Error())
			}
			return
		}
	}
}

func (c *Client) Request(method, path string, opts RequestOptions) (*http.Request, error) {
	qs, err := opts.Querystring()
	if err != nil {
		return nil, err
	}

	r, err := opts.Reader()
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s://%s%s%s?%s", c.Endpoint.Scheme, c.Endpoint.Host, c.Endpoint.Path, path, qs)

	req, err := http.NewRequest(method, endpoint, r)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Accept", "*/*")
	req.Header.Set("Content-Type", opts.ContentType())

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	if c.Endpoint.User != nil {
		pw, _ := c.Endpoint.User.Password()
		req.SetBasicAuth(c.Endpoint.User.Username(), pw)
	}

	if c.Prepare != nil {
		c.Prepare(req)
	}

	return req, nil
}

func (c *Client) handleRequest(req *http.Request) (*http.Response, error) {
	res, err := DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if err := responseError(res); err != nil {
		return nil, err
	}

	return res, nil
}

func responseError(res *http.Response) error {
	// disabled because HTTP2 over ALB doesnt work yet

	// if !res.ProtoAtLeast(2, 0) {
	//   return fmt.Errorf("server did not respond with http/2")
	// }

	if res.StatusCode < 400 {
		return nil
	}

	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}

	var e struct {
		Error string
	}

	if err := json.Unmarshal(data, &e); err == nil && e.Error != "" {
		return fmt.Errorf(e.Error)
	}

	msg := strings.TrimSpace(string(data))

	if len(msg) > 0 {
		return fmt.Errorf(msg)
	}

	return fmt.Errorf("response status %d", res.StatusCode)
}

func unmarshalReader(r io.ReadCloser, out interface{}) error {
	defer r.Close()

	if out == nil {
		return nil
	}

	data, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, out)
}

func MarshalOptions(opts interface{}) (RequestOptions, error) {
	ro := RequestOptions{
		Headers: Headers{},
		Params:  Params{},
		Query:   Query{},
	}

	v := reflect.ValueOf(opts)
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)

		if n := f.Tag.Get("header"); n != "" {
			if u := marshalValue(v.Field(i)); u != nil {
				if uv, ok := u.(string); ok {
					ro.Headers[n] = uv
				}
			}
		}

		if n := f.Tag.Get("param"); n != "" {
			if u := marshalValue(v.Field(i)); u != nil {
				ro.Params[n] = u
			}
		}

		if n := f.Tag.Get("query"); n != "" {
			if u := marshalValue(v.Field(i)); u != nil {
				ro.Query[n] = u
			}
		}
	}

	return ro, nil
}

func marshalValue(f reflect.Value) interface{} {
	if f.IsNil() {
		return nil
	}

	if f.Kind() == reflect.Ptr {
		return f.Elem().Interface()
	}

	return f.Interface()
}
