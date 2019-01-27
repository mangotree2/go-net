package meta

import (
	"bufio"
	"bytes"
	"errors"
	"strings"

	//"io"
	"net/http"
	"net/textproto"
)

type Meta map[string][]string

const MetaErr = "X-Reply-Error"

func (m Meta) Marshal() ([]byte,error) {

	header := make(http.Header)

	for k, v := range m {
		for _,_v := range v {
			header.Add(k, _v)
		}
	}

	buf := new(bytes.Buffer)

	err := header.Write(buf)
	if err != nil {
		return nil,err
	}
	buf.WriteString("\n\n")
	return buf.Bytes(),nil


}

func (m Meta) UnMarshal(buf []byte) (error) {
	tp := textproto.NewReader(bufio.NewReader(strings.NewReader((string(buf)))))

	mh ,err := tp.ReadMIMEHeader()
	if err != nil {
		return  err
	}

	//log.Println(mh)

	for k,v := range mh {
		//log.Println(k,v)
		m[k] = v

	}

	return nil

}

func (m Meta)Add(key,value string) {
	m[key] = append(m[key], value)
}

func (m Meta)Set(key,value string) {
	m[key] = []string{value}
}

func (m Meta) Peek(key string) string {
	if m == nil {
		return ""
	}

	v := m[key]

	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func (m Meta) VisitAll(f func(key, value string)) {
	for k,v := range m {
		if len(v) > 0 {
			f(k,v[0])
		}
	}
}

func (m Meta)CopyTo() Meta {
	m2 := make(Meta, len(m))
	for k, vv := range m {
		vv2 := make([]string, len(vv))
		copy(vv2, vv)
		m2[k] = vv2
	}


	return m2
}

func (m Meta) SetErrToMeta(err string) {
	m.Set(MetaErr,err)
}

func (m Meta) ErrFromMeta() error {
	v := m.Peek(MetaErr)
	if len(v) == 0 {
		return nil
	}

	return errors.New(v)
}

func NewMeta () Meta {

	m := make(map[string][]string)
	return Meta(m)
}

