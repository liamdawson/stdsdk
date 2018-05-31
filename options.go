package stdsdk

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"time"
)

type Headers map[string]string
type Params map[string]interface{}
type Query map[string]interface{}

type RequestOptions struct {
	Body    io.Reader
	Headers Headers
	Params  Params
	Query   Query
}

func (o *RequestOptions) Querystring() (string, error) {
	u, err := marshalValues(o.Query)
	if err != nil {
		return "", err
	}

	return u.Encode(), nil
}

func (o *RequestOptions) Reader() (io.Reader, error) {
	if o.Body != nil && len(o.Params) > 0 {
		return nil, fmt.Errorf("cannot specify both Body and Params")
	}

	if o.Body == nil && len(o.Params) == 0 {
		return bytes.NewReader(nil), nil
	}

	if o.Body != nil {
		return o.Body, nil
	}

	u, err := marshalValues(o.Params)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader([]byte(u.Encode())), nil
}

func (o *RequestOptions) ContentType() string {
	if o.Body == nil {
		return "application/x-www-form-urlencoded"
	}

	return "application/octet-stream"
}

func marshalValues(vv map[string]interface{}) (url.Values, error) {
	u := url.Values{}

	for k, v := range vv {
		switch t := v.(type) {
		case bool:
			u.Set(k, fmt.Sprintf("%t", t))
		case int:
			u.Set(k, fmt.Sprintf("%d", t))
		case string:
			u.Set(k, t)
		case []string:
			for _, s := range t {
				u.Add(k, s)
			}
		case time.Duration:
			u.Set(k, t.String())
		default:
			return nil, fmt.Errorf("unknown param type: %T", t)
		}
	}

	return u, nil
}
