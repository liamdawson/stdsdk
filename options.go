package stdsdk

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strconv"
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
			if u, ok := marshalValue(v.Field(i)); ok {
				ro.Headers[n] = u
			}
		}

		if n := f.Tag.Get("param"); n != "" {
			if u, ok := marshalValue(v.Field(i)); ok {
				ro.Params[n] = u
			}
		}

		if n := f.Tag.Get("query"); n != "" {
			if u, ok := marshalValue(v.Field(i)); ok {
				ro.Query[n] = u
			}
		}
	}

	return ro, nil
}

func marshalValue(f reflect.Value) (string, bool) {
	if f.IsNil() {
		return "", false
	}

	v := f.Interface()

	if f.Kind() == reflect.Ptr {
		v = f.Elem().Interface()
	}

	switch t := v.(type) {
	case bool:
		return fmt.Sprintf("%t", t), true
	case int:
		return strconv.Itoa(t), true
	case string:
		return t, true
	case time.Duration:
		return t.String(), true
	case map[string]string:
		uv := url.Values{}
		for k, v := range t {
			uv.Add(k, v)
		}
		return uv.Encode(), true
	default:
		return "", false
	}

	return "", true
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
