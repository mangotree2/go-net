package transport

import (
	"context"
	"fmt"
	"github.com/mangotree2/go-net/codec"
	"github.com/mangotree2/go-net/net-warp"
	"github.com/mangotree2/go-net/util/meta"
	"log"
	"reflect"
	"sync"
	"time"
)

type (
	PreCtx interface {
		TranSport () TranSport
		Session() Session
		Ip() string
		RealIp() string
		Swap() sync.Map
		Context() context.Context
	}

	inputCtx interface {
		PreCtx
		Seq() int32
		PeekMeta(key string) string
		VisitMeta(f func(key, value string))
		Uri() string
		//UriObject() *url.URL
		ResetUri(string)
		Path() string
		//Query() url.Values

	}

	CallCtx interface {
		inputCtx
		Input() *Message
		GetBodyCodec() byte
		Output() *Message
		ReplyBodyCodec() byte
		SetBodyCodec(byte)
		AddMeta(key,value string)
		SetMeta(key,value string)

	}

	PushCtx interface {
		inputCtx
		GetBodyCodec() byte
	}

	UnknownCallCtx interface {
		inputCtx
		GetBodyCodec() byte
		InputBodyBytes() []byte
		Bind(v interface{}) (bodyCodec byte,err error)
	}

	UnknownPushCtx interface {
		inputCtx
		GetBodyCodec() byte
		InputBodyBytes() []byte
		Bind(v interface{}) (bodyCodec byte,err error)
	}


	handlerCtx struct {

		sess *session
		input *Message
		output *Message
		handler *Handler
		arg reflect.Value//请求参数
		callCmd *callCmd//call cmd
		swap sync.Map
		start time.Time
		cost  time.Duration
		handlerErr error
		context context.Context
		next *handlerCtx
	}

)

func (c *handlerCtx) InputBodyBytes() []byte {
	b, ok := c.input.Body().(*[]byte)
	if !ok {
		return nil
	}
	return *b
}


func (c *handlerCtx) Bind(v interface{}) (bodyCodec byte, err error) {
	b := c.InputBodyBytes()
	if b == nil {
		return codec.NilCodecId,nil
	}
	c.input.SetBody(v)
	err = c.input.UnmarshalBody(b)
	return c.input.BodyCodec(),err
}

func (c *handlerCtx) TranSport() TranSport {
	return c.sess.transport
}

func (c *handlerCtx) Session() Session {
	return c.sess
}

func (c *handlerCtx) Ip() string {
	return c.sess.RemoteAddr().String()
}

func (c *handlerCtx) RealIp() string {
	realIp := c.PeekMeta("x-real-ip")
	if len(realIp) > 0 {
		return realIp
	}
	return c.sess.RemoteAddr().String()
}

func (c *handlerCtx) Swap() sync.Map {
	return c.swap
}

func (c *handlerCtx) Context() context.Context {
	if c.context == nil {
		return c.input.Context()
	}
	return c.context
}

func (c *handlerCtx) Seq() int32 {
	return c.input.Seq()
}

func (c *handlerCtx) PeekMeta(key string) string {
	return c.input.Meta().Peek(key)
}

func (c *handlerCtx) VisitMeta(f func(key, value string)) {
	c.input.Meta().VisitAll(f)
}

func (c *handlerCtx) Uri() string {
	return c.input.Uri()
}

func (c *handlerCtx) ResetUri(uri string) {
	c.input.SetUri(uri)
}

func (c *handlerCtx) Path() string {
	return c.input.UriObject().Path
}

func (c *handlerCtx) Input() *Message {
	return c.input
}

func (c *handlerCtx) Output() *Message {
	return c.output
}

func (c *handlerCtx) SetBodyCodec(bodyCodec byte) {
	c.output.SetBodyCodec(bodyCodec)
}

func (c *handlerCtx) AddMeta(key, value string) {
	c.output.Meta().Add(key,value)
}

func (c *handlerCtx) SetMeta(key, value string) {
	c.output.Meta().Set(key,value)
}

func (c *handlerCtx)GetBodyCodec() byte {
	return c.input.BodyCodec()
}


var ctxPool = sync.Pool{
	New: func() interface{} {
		return newReadHandleCtx()
	},
}


func newReadHandleCtx() *handlerCtx {
	c := new(handlerCtx)
	c.input = socket.NewMessage()
	c.input.SetNewBody(c.binding)
	c.output = socket.NewMessage()
	return c
}

func (c *handlerCtx)binding(header Header) (body interface{}) {
	c.start = c.sess.timeNow()
	//todo plugin
	switch header.Mtype() {
	case TypeReply:
		return c.bindReply(header)
	case TypePush:
		return c.bindPush(header)
	case TypeCall:
		return c.bindCall(header)
	default:
		c.handlerErr = errCodeMtypeNotAllowed
		return nil
	}

}

func (c *handlerCtx) bindReply(header Header) interface{} {
	_callCmd, ok := c.sess.callCmdMap.Load(header.Seq())
	if !ok {
		fmt.Printf("not found call cmd :%v",c.input)
		return nil

	}

	c.callCmd = _callCmd.(*callCmd)

	//unlock:handleReply
	c.callCmd.mu.Lock()
	c.input.SetUri(c.callCmd.output.Uri())
	c.swap = c.callCmd.swap
	c.callCmd.inputBodyCodec = c.GetBodyCodec()
	//c.callCmd.inputMeta = meta.NewMeta()
	c.callCmd.inputMeta = c.input.Meta().CopyTo()
	c.setContext(c.callCmd.output.Context())
	c.input.SetBody(c.callCmd.result)

	//todo plugin

	return c.input.Body()

}

func (c *handlerCtx) bindCall(header Header) interface{} {
	//todo plugin

	u := header.UriObject()
	if len(u.Path) == 0 {
		c.handlerErr = errBadMessage
		return nil
	}

	var ok bool
	c.handler, ok = c.sess.getCallHandler(u.Path)
	if !ok {
		c.handlerErr = errNotFound
		return nil
	}

	//

	if c.handler.isUnknown {
		c.input.SetBody(new([]byte))
	} else {
		c.arg = c.handler.NewArgValue()
		c.input.SetBody(c.arg.Interface())
	}


	return c.input.Body()
}

func (c *handlerCtx)bindPush(header Header) interface{} {
	//todo plugin

	u := header.UriObject()
	if len(u.Path) == 0 {
		c.handlerErr =  errBadMessage
		return nil
	}

	var ok bool
	c.handler, ok = c.sess.getPushHandler(u.Path)
	if !ok {
		c.handlerErr = errNotFound
		return nil
	}

	c.arg = c.handler.NewArgValue()
	c.input.SetBody(c.arg.Interface())

	// plugin

	return c.input.Body()
}

func (c *handlerCtx) handle() {
	if c.handlerErr != nil && c.handlerErr == errCodeMtypeNotAllowed {
		goto E
	}

	switch c.input.Mtype() {
	case TypeReply:
		c.handleReply()
		return
	case TypeCall:
		c.handleCall()
		return
	case TypePush:
		c.handlePush()
		return
	default:
	}

E:

	//errCodeMtypeNotAllowed.SetToMeta(c.output.Meta())
	c.output.Meta().SetErrToMeta(errCodeMtypeNotAllowed.Error())
	fmt.Printf("handle err")
	go c.sess.Close()
}

func (c *handlerCtx) handleReply() {
	if c.callCmd == nil {
		return
	}

	//lock :bindReply
	defer c.callCmd.mu.Unlock()
	defer func() {
		if p := recover(); p != nil {
			log.Println("handle reply err:",p)
		}
		c.callCmd.result = c.input.Body()
		c.handlerErr = c.callCmd.err
		c.callCmd.done()
		c.callCmd.cost = c.sess.timeSince(c.callCmd.start)
		//todo print

	}()

	if c.callCmd.err != nil {
		return
	}
	err := c.input.Meta().ErrFromMeta()
	c.callCmd.err = err

}

func (c *handlerCtx)handleCall() {
	var writed bool
	defer func() {
		if p := recover(); p != nil {
			log.Println("panic handle call err:",p)
			if !writed{
				if c.handlerErr == nil {
					c.handlerErr = errInternalServerErr
				}
				c.writeReply(c.handlerErr)
			}

		}

		c.cost = c.sess.timeSince(c.start)
	//
	}()

	c.output.SetMtype(TypeReply)
	c.output.SetSeq(c.input.Seq())
	c.output.SetUriObject(c.input.UriObject())

	if age := c.sess.ContextAge(); age > 0 {
		ctxTimeout ,_ := context.WithTimeout(c.input.Context(),age)
		c.setContext(ctxTimeout)
		socket.WithContext(ctxTimeout)(c.output)
	}

	if c.handlerErr == nil {
		c.handlerErr = c.output.Meta().ErrFromMeta()
	}

	if c.handlerErr == nil {
		// todo plugin
		if c.handler.isUnknown {
			c.handler.unKnownHandleFunc(c)
		} else {
			c.handler.handleFunc(c,c.arg)
		}

	}

	c.setReplyBodyCodec(c.handlerErr != nil)
	//todo plugin

	err := c.writeReply(c.handlerErr)
	if err != nil {
		if c.handlerErr == nil {
			c.handlerErr = err
		}
		if err != errConnClosed {
			c.writeReply(errInternalServerErr)
		}
		return
	}

	writed = true

}

func (c *handlerCtx) handlePush() {
	if age := c.sess.ContextAge(); age > 0 {
		ctxTimeout, _ := context.WithTimeout(context.Background(),age)
		c.setContext(ctxTimeout)
	}

	defer func() {
		if p := recover(); p != nil {
			fmt.Printf("panic handle push,err :%v\n",p)
		}

		c.cost = c.sess.timeSince(c.start)
		//print
	}()


	if c.handlerErr == nil && c.handler != nil {
		if c.handler.isUnknown {
			c.handler.unKnownHandleFunc(c)
		} else {
			c.handler.handleFunc(c,c.arg)
		}
	}

	if c.handlerErr!= nil {
		fmt.Printf("handler push err :%v\n",c.handlerErr.Error())
	}

}




func (c *handlerCtx) clean() {
	c.sess = nil
	c.handlerErr = nil
	c.arg = reflect.Value{}
	//c.callCmd = nil
	c.swap = sync.Map{}
	c.cost = 0
	c.context = nil
	c.input.Reset(socket.WithNewBody(c.binding))
	c.output.Reset()
}

func (c *handlerCtx) reInit(s *session) {
	c.sess = s
	//count := s.socket.Sw
	s.socket.Swap().Range(func(key, value interface{}) bool {
		c.swap.Store(key,value)
		return true
	})

}


func (c *handlerCtx)setContext(ctx context.Context) {
	c.context = ctx
}

func (c *handlerCtx)writeReply(err error) error {
	if err != nil {
		c.output.Meta().SetErrToMeta(err.Error())
		c.output.SetBody(nil)
		c.output.SetBodyCodec(codec.NilCodecId)
	}

	uri := c.output.Uri()
	c.output.SetUri("")
	_,err = c.sess.write(c.output)
	c.output.SetUri(uri)
	return err


}

func (c *handlerCtx) setReplyBodyCodec(hasError bool)  {
	if hasError {
		return
	}
	c.ReplyBodyCodec()
}

func (c *handlerCtx)ReplyBodyCodec() byte {
	id := c.output.BodyCodec()
	if id != codec.NilCodecId {
		return id
	}

	id, ok := GetAcceptBodyCodec(c.input.Meta())
	if ok  {
		if _, err := codec.Get(id); err == nil {
			c.output.SetBodyCodec(id)
			return id
		}
	}

	id = c.input.BodyCodec()
	c.output.SetBodyCodec(id)

	return id
}



/////////////////////////////////////////////
type (
	CallCmd interface {
		TraceTransport() (TranSport,bool)
		TraceSession() (Session,bool)
		Context() context.Context
		Output() *Message
		Error() error

		Done() <-chan struct{}

		Reply() (interface{},error)
		InputBodyCodec() byte
		InputMeta() meta.Meta
		CostTime() time.Duration
	}

	callCmd struct {
		sess *session
		output *Message
		result interface{}
		err error
		inputBodyCodec byte
		inputMeta meta.Meta
		start time.Time
		cost time.Duration
		swap sync.Map
		mu sync.Mutex

		//
		callCmdChan chan<- CallCmd

		//
		doneChan chan struct{}
	}
)

func (c *callCmd) TraceTransport() (TranSport,bool) {
	return c.Transport(), true
}

func (c *callCmd) Transport() (TranSport) {
	return c.sess.transport
}

func (c *callCmd) TraceSession() (Session,bool) {
	return c.Session(),true
}

func (c *callCmd) Session() Session {
	return c.sess
}

func (c *callCmd) Context() context.Context {
	return c.output.Context()
}

func (c *callCmd) Output() *Message {
	return c.output
}

func (c *callCmd) Error() error {
	return c.err
}

func (c *callCmd) Done() <-chan struct{} {
	return c.doneChan
}

func (c *callCmd) Reply() (interface{}, error) {
	<-c.Done()
	return c.result, c.err
}

func (c *callCmd) InputBodyCodec() byte {
	<-c.Done()
	return c.inputBodyCodec
}

func (c *callCmd) InputMeta() meta.Meta {
	<-c.Done()
	return c.inputMeta
}

func (c *callCmd) CostTime() time.Duration {
	<-c.Done()
	return c.cost
}

func (c *callCmd) done() {
	c.sess.callCmdMap.Delete(c.output.Seq())

	c.callCmdChan <- c
	close(c.doneChan)

	c.sess.graceCallCmdWaitGroup.Done()
}

func (c *callCmd)hasReply() bool {
	return c.inputMeta != nil
}

func (c *callCmd) cancel() {
	c.sess.callCmdMap.Delete(c.output.Seq())
	c.err = errConnClosed
	c.callCmdChan <- c
	close(c.doneChan)
	c.sess.graceCallCmdWaitGroup.Done()
}

