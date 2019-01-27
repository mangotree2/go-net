package socket

import (
	"context"
	"github.com/mangotree2/go-net/codec"
	"net/url"
	"github.com/mangotree2/go-net/util/meta"

	"fmt"
	"math"
	"sync"
)

type Message struct {
		//
		seq int32
		//  CALL, PUSH, REPLY
		mtype byte
		// URI string
		uri string
		// URI object
		uriObject *url.URL
		// metadata
		//meta utils.Args
		meta meta.Meta
		// body codec type
		bodyCodec byte
		// body object
		body interface{}
		//
		newBodyFunc NewBodyFunc
		//
		size uint32
		// ctx是消息处理上下文，带有截止日期，取消信号以及跨越API边界的其他值。
		ctx context.Context
		// stack
		next *Message
}

type Header interface {
	//消息序列
	Seq() int32
	//
	SetSeq(int32)
	//
	Mtype() byte
	//
	SetMtype(byte)
	//
	Uri() string
	//UriObject  returns the URI object
	UriObject() *url.URL
	//
	SetUri(string)
	//
	SetUriObject(*url.URL)
	//
	Meta() meta.Meta


}

type Body interface {
	//返回编解码类型
	BodyCodec() byte
	//
	SetBodyCodec(byte)
	//
	Body() interface{}
	//
	SetBody(body interface{})
	//重置获取body的功能。
	SetNewBody(newBodyFunc NewBodyFunc)
	//
	MarshalBody() ([]byte, error)
	//
	UnmarshalBody(bodyBytes []byte) error
}

type NewBodyFunc func(Header) interface{}

var (
	_ Header = new(Message)
	_ Body   = new(Message)
)

//单例 模拟栈
var messageStack = new(struct {
	freeMessage *Message
	mu          sync.Mutex
})


// Message设置器
type MessageSetting func(*Message)

func GetMessage(setting ...MessageSetting) *Message {
	messageStack.mu.Lock()
	m := messageStack.freeMessage
	if m == nil {
		m = NewMessage(setting...)

	} else {
		messageStack.freeMessage = m.next
		m.doSetting(setting...)
	}

	messageStack.mu.Unlock()

	return m

}

func PutMessage(m *Message) {
	if m != nil {
		messageStack.mu.Lock()
		m.Reset()
		m.next = messageStack.freeMessage
		messageStack.freeMessage = m
		messageStack.mu.Unlock()
	}
}


////////Message////////
func NewMessage(settings ...MessageSetting) *Message {
	var m = &Message{
		//meta: sync.Map{},
		meta:meta.NewMeta(),
	}
	m.doSetting(settings...)
	return m
}

func (m *Message) doSetting(settings ...MessageSetting) {
	for _, fn := range settings {
		if fn != nil {
			fn(m)
		}
	}
}

func (m *Message) Reset(setting ...MessageSetting) {
	m.next = nil
	m.body = nil
	m.meta = meta.NewMeta()
	m.newBodyFunc = nil
	m.seq = 0
	m.mtype = 0
	m.uri = ""
	m.uriObject = nil
	m.size = 0
	m.ctx = nil
	m.bodyCodec = 0
	m.doSetting(setting...)

}

func (m *Message) Context() context.Context {
	if m.ctx == nil {
		return context.Background()
	}

	return m.ctx
}

func (m *Message) Seq() int32 {
	return m.seq
}

func (m *Message) SetSeq(seq int32) {
	m.seq = seq
}

func (m *Message) Mtype() byte {
	return m.mtype
}

func (m *Message) SetMtype(mtype byte)  {
	m.mtype = mtype
}

func (m *Message) Uri() string {
	if m.uriObject != nil {
		return m.uriObject.String()
	}
	return m.uri
}

func (m *Message) UriObject() *url.URL {
	if m.uriObject == nil {
		m.uriObject, _ = url.Parse(m.uri)
		if m.uriObject == nil {
			m.uriObject = new(url.URL)
		}
		m.uri = ""
	}
	return m.uriObject
}

func (m *Message) SetUri(uri string) {
	m.uri = uri
	m.uriObject = nil
}

func (m *Message) SetUriObject(uriObject *url.URL) {
	m.uriObject = uriObject
	m.uri = ""
}

func (m *Message) Meta() meta.Meta {
	return m.meta
}

func (m *Message) BodyCodec() byte {
	return m.bodyCodec
}

func (m *Message) SetBodyCodec(bodyCodec byte) {
	m.bodyCodec = bodyCodec
}

func (m *Message) Body() interface{} {
	return m.body
}

func (m *Message) SetBody(body interface{}) {
	m.body = body
}

func (m *Message) SetNewBody(newBodyFunc NewBodyFunc) {
	m.newBodyFunc = newBodyFunc
}

func (m *Message) MarshalBody() ([]byte, error) {
	switch body := m.body.(type) {
	default:
		//取其编码方式去编码
		c,err := codec.Get(m.bodyCodec)
		if err != nil {
			return []byte{},err
		}
		return c.Marshal(body)
	case nil:
		return []byte{}, nil
	case *[]byte:
		if body == nil {
			return []byte{}, nil
		}
		return *body, nil
	case []byte:
		return body, nil
	}

}

func (m *Message) UnmarshalBody(bodyBytes []byte) error {
	if m.body == nil && m.newBodyFunc != nil {
		//对应bind
		m.body = m.newBodyFunc(m)
	}

	length := len(bodyBytes)
	if length == 0 {
		return nil
	}

	switch body := m.body.(type) {
	default:
		c, err := codec.Get(m.bodyCodec)
		if err != nil {
			return err
		}
		return c.UnMarshal(bodyBytes,m.body)
	case nil :
		return nil
	case *[]byte:
		if cap(*body) < length {
			*body = make([]byte,length)
		} else {
			*body = (*body)[:length]
		}
		copy(*body,bodyBytes)
		return nil
	}


}


func (m *Message) Size() uint32 {
	return m.size
}

func (m *Message) SetSize(size uint32) error {
	if size > math.MaxUint32 {
		return fmt.Errorf("size to big")
	}
	m.size = size
	return nil
}

func WithContext(ctx context.Context) MessageSetting {
	return func(message *Message) {
		message.ctx = ctx
	}
}

func WithSeq(seq int32) MessageSetting {
	return func(m *Message) {
		m.seq = seq
	}
}


func WithMtype(mtype byte) MessageSetting {
	return func(m *Message) {
		m.mtype = mtype
	}
}

func WithUri(uri string) MessageSetting {
	return func(m *Message) {
		m.SetUri(uri)
	}
}

func WithUriObject(uriObject *url.URL) MessageSetting {
	return func(m *Message) {
		m.SetUriObject(uriObject)
	}
}

func WithQuery(key, value string) MessageSetting {
	return func(m *Message) {
		u := m.UriObject()
		v := u.Query()
		v.Add(key, value)
		u.RawQuery = v.Encode()
	}
}

func WithAddMeta(key, value string) MessageSetting {
	return func(m *Message) {
		//m.meta.Store(key, value)
		m.meta.Add(key,value)

	}
}

func WithSetMeta(key, value string) MessageSetting {
	return func(m *Message) {
		//m.meta.Store(key, value)
		m.meta.Set(key,value)
	}
}

func WithBodyCodec(bodyCodec byte) MessageSetting {
	return func(m *Message) {
		m.bodyCodec = bodyCodec
	}
}


func WithBody(body interface{}) MessageSetting {
	return func(m *Message) {
		m.body = body
	}
}

func WithNewBody(newBodyFunc NewBodyFunc) MessageSetting {
	return func(m *Message) {
		m.newBodyFunc = newBodyFunc
	}
}