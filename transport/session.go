package transport

import (
	"context"
	"fmt"
	"github.com/mangotree2/go-net/codec"
	"github.com/mangotree2/go-net/net-warp"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	statusOk            int32 = 0
	statusActiveClosing int32 = 1
	statusActiveClosed  int32 = 2
	statusPassiveClosed int32 = 3
)


type (
	Session interface {

		SetId(newId string)
		Close() error
		CloseNotify() <-chan struct{}
		Health() bool

		AsyncCall(uri string,arg interface{},result interface{},callCmdChan chan<- CallCmd, setting ...MessageSetting) CallCmd

		Call(uri string, arg interface{}, result interface{}, setting ...MessageSetting) CallCmd

		Push(uri string, arg interface{}, setting ...MessageSetting) error

		SessionAge() time.Duration

		ContextAge() time.Duration
	}
)


type session struct {
	transport *transport
	getCallHandler, getPushHandler func(uriPath string)(*Handler,bool)
	timeSince	func(time.Time) time.Duration
	timeNow 	func() time.Time
	seq 		int32
	seqLock		sync.Mutex
	callCmdMap  sync.Map
	protoFuncs  []ProtoFunc
	socket		socket.Socket
	status 		int32 // 0:ok, 1:active closed, 2:disconnect
	closeNotifyCh chan struct{}
	didCloseNotify int32
	statusLock 	sync.Mutex
	writeLock   sync.Mutex
	graceCtxWaitGroup sync.WaitGroup
	graceCallCmdWaitGroup sync.WaitGroup
	sessionAge 	time.Duration
	contextAge	time.Duration
	sessionAgeLock sync.RWMutex
	contextAgeLock sync.RWMutex
	conn net.Conn
	lock 	sync.RWMutex

	//client
	redialForClientLocked func(old net.Conn) bool

}



func newSession(transport *transport,conn net.Conn,protoFuncs []ProtoFunc) *session {
	var s = &session{
		transport:             transport,
		getCallHandler:        transport.router.subRouter.getCall,
		getPushHandler:        transport.router.subRouter.getPush,
		timeSince:             transport.timeSince,
		timeNow:               transport.timeNow,
		callCmdMap:            sync.Map{},
		protoFuncs:            protoFuncs,
		socket:                socket.NewSocket(conn,protoFuncs...),
		status:                0,
		closeNotifyCh:         make(chan struct{}),
		sessionAge:            transport.defaultSessionAge,
		contextAge:            transport.defaultContextAge,
		conn:                  conn,
	}

	return s
}

func (s *session)Id() string {
	return s.socket.Id()
}

func (s *session) SessionAge() time.Duration {
	s.sessionAgeLock.RLock()
	age := s.sessionAge
	s.sessionAgeLock.RUnlock()
	return age
}

func (s *session)ContextAge() time.Duration {
	s.contextAgeLock.RLock()
	age := s.contextAge
	s.contextAgeLock.RUnlock()
	return age
}


func (s *session)getConn() net.Conn {
	s.lock.RLock()
	c := s.conn
	s.lock.RUnlock()
	return c
}

func (s *session) getStatus() int32 {
	return atomic.LoadInt32(&s.status)
}

func (s *session)activelyClosing() {
	atomic.StoreInt32(&s.status,statusActiveClosing)
}

func (s *session) acticelyClosed() {
	atomic.StoreInt32(&s.status,statusPassiveClosed)
}

func (s *session)goonRead() bool {
	stauts := atomic.LoadInt32(&s.status)
	return stauts == statusOk || stauts == statusActiveClosing
}

func (s *session) IsPassiveClosed() bool {
	return atomic.LoadInt32(&s.status) == statusPassiveClosed
}

func (s *session)passivelyClosd() {
	atomic.StoreInt32(&s.status,statusPassiveClosed)
}

func (s *session) RemoteAddr() net.Addr {
	return  s.socket.RemoteAddr()
}

func (s *session) LocalAddr() net.Addr {
	return s.socket.LocalAddr()
}

func (s *session) startReadAndHandle() {
	var withContext MessageSetting
	if readTimeout := s.SessionAge(); readTimeout > 0 {
		s.socket.SetReadDeadline(time.Now().Add(readTimeout))
		ctxTimeout,_ := context.WithTimeout(context.Background(),readTimeout)
		withContext = socket.WithContext(ctxTimeout)
	} else {
		s.socket.SetReadDeadline(time.Time{})
		withContext = socket.WithContext(nil)
	}

	var (
		err error
		conn = s.getConn()
	)

	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("")
		}
		//todo 错误处理

		s.readDisconnected(conn,err)

	}()

	for s.goonRead(){
		var ctx = s.transport.getContext(s,false)
		withContext(ctx.input)

		//todo preRead

		err = s.socket.ReadMessage(ctx.input)
		if (err != nil && ctx.GetBodyCodec() == codec.NilCodecId) || !s.goonRead() {
			s.transport.putContext(ctx,false)
			return
		}

		if err != nil {
			ctx.handlerErr = errBadMessage
		}

		s.graceCtxWaitGroup.Add(1)

		go func() {

			defer func() {
				s.transport.putContext(ctx, true)
			}()
			ctx.handle()

		}()

	}


}

func (s *session) write(message *Message) (net.Conn,error){
	conn := s.getConn()
	status := s.getStatus()

	if status != statusOk &&
		!(status == statusActiveClosing && message.Mtype() == TypeReply) {
			return conn, errConnClosed
	}

	var (
		err error
		ctx = message.Context()
		deadline,_ = ctx.Deadline()
	)

	select {
	case <- ctx.Done():
		err = ctx.Err()
		goto ERR
	default:
	}

	s.writeLock.Lock()
	defer s.writeLock.Unlock()

	select {
	case <-ctx.Done():
		err = ctx.Err()
		goto ERR
	default:
		s.socket.SetWriteDeadline(deadline)
		err = s.socket.WriteMessage(message)
	}

	if err == nil {
		return conn,nil
	}

	if err == io.EOF || err == socket.ErrProactivelyCloseSocket {
		return conn, errConnClosed
	}


ERR:
	return conn,err

}

func (s *session)notifyClose() {
	if atomic.CompareAndSwapInt32(&s.didCloseNotify,0,1) {
		close(s.closeNotifyCh)
	}
}

func (s *session) Close() error {
	s.lock.Lock()

	s.statusLock.Lock()

	status := s.getStatus()
	if status != statusOk {
		s.statusLock.Unlock()
		s.lock.Unlock()
		return nil
	}

	s.activelyClosing()
	s.statusLock.Unlock()

	s.transport.sessHub.Delete(s.Id())
	s.notifyClose()

	s.graceCtxWaitGroup.Wait()
	s.graceCallCmdWaitGroup.Wait()

	s.statusLock.Lock()

	if !s.IsPassiveClosed() {
		s.acticelyClosed()
	}

	s.statusLock.Unlock()

	err := s.socket.Close()
	s.lock.Unlock()

	return err



}

func (s *session)readDisconnected(oldConn net.Conn,err error) {
	s.statusLock.Lock()
	status := s.getStatus()
	if status == statusActiveClosed {
		s.statusLock.Unlock()
		return
	}

	s.passivelyClosd()
	s.statusLock.Unlock()

	s.transport.sessHub.Delete(s.Id())
	s.notifyClose()

	if err != nil && err != io.EOF && err != socket.ErrProactivelyCloseSocket {
		fmt.Printf("disconnect(%s), when reading :%s ",s.RemoteAddr().String(),err.Error())
	}
	s.graceCtxWaitGroup.Wait()

	s.callCmdMap.Range(func(_, value interface{}) bool {
		callCmd := value.(*callCmd)
		callCmd.mu.Lock()
		if !callCmd.hasReply() && callCmd.err == nil {
			callCmd.cancel()
		}
		callCmd.mu.Unlock()
		return true
	})

	if status == statusActiveClosing {
		return
	}

	s.socket.Close()

	if !s.redialForClientLocked(oldConn) {
		//
	}

}

//算是client

func (s *session) redialForClient(oldConn net.Conn) bool {
	if s.redialForClientLocked == nil {
		return false
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	status := s.getStatus()
	if status == statusActiveClosing || status == statusActiveClosed {
		return false
	}


	return s.redialForClientLocked(oldConn)
}

func (s *session) AsyncCall(uri string, arg interface{}, result interface{}, callCmdChan chan<- CallCmd, setting ...MessageSetting) CallCmd {
	if callCmdChan == nil {
		callCmdChan = make(chan CallCmd,10)
	} else {

		if cap(callCmdChan) == 0 {
			log.Fatalf("seesion asynCall channel is no enough buffer")
		}
	}

	output := socket.NewMessage(
			socket.WithMtype(TypeCall),
			socket.WithUri(uri),
			socket.WithBody(arg),
		)

	for _,fn := range setting {
		if fn != nil {
			fn(output)
		}
	}

	seq := output.Seq()
	if seq == 0 {
		s.seqLock.Lock()
		s.seq++
		seq = s.seq
		output.SetSeq(seq)
		s.seqLock.Unlock()
	}

	if output.BodyCodec() == codec.NilCodecId {
		output.SetBodyCodec(s.transport.defaultBodyCodec)
	}

	if age := s.ContextAge(); age >0 {
		ctxTimeOut, _ := context.WithTimeout(output.Context(),age)
		socket.WithContext(ctxTimeOut)(output)
	}

	cmd := &callCmd{
		sess: s,
		output: output,
		result:result,
		callCmdChan:callCmdChan,
		doneChan:make(chan struct{}),
		start:s.transport.timeNow(),
		swap:sync.Map{},
	}

	s.graceCallCmdWaitGroup.Add(1)

	//todo len
	s.socket.Swap().Range(func(key, value interface{}) bool {

		cmd.swap.Store(key,value)
		return true
	})

	cmd.mu.Lock()
	defer cmd.mu.Unlock()

	s.callCmdMap.Store(seq,cmd)

	defer func() {
		if p := recover(); p != nil {
			log.Printf("panic :%v",p)
		}
	}()

	//todo plugin

	var usedConn net.Conn
W:
	if usedConn,cmd.err = s.write(output);cmd.err != nil {
		if cmd.err == errConnClosed && s.redialForClient(usedConn) {
			goto W
		}
		cmd.done()

		return cmd
	}

	return cmd

}

func (s *session) SetId(newId string) {
	oldId := s.Id()
	if oldId == newId {
		return
	}
	s.socket.SetId(newId)
	hub := s.transport.sessHub
	hub.Set(s)
	hub.Delete(oldId)
}

// CloseNotify返回一个在连接消失后关闭的通道。
func (s *session) CloseNotify() <-chan struct{} {
	return s.closeNotifyCh
}

func (s *session) Health() bool {
	status := s.getStatus()
	if status == statusOk {
		return true
	}
	if s.redialForClientLocked == nil {
		return false
	}
	if status == statusPassiveClosed {
		return true
	}
	return false
}

func (s *session) Call(uri string, arg interface{}, result interface{}, setting ...MessageSetting) CallCmd {
	callCmd := s.AsyncCall(uri,arg,result,make(chan CallCmd,1),setting...)
	<-callCmd.Done()
	return callCmd
}

func (s *session) Push(uri string, arg interface{}, setting ...MessageSetting) error {
	ctx := s.transport.getContext(s,true)
	ctx.start = s.transport.timeNow()
	output := ctx.output
	output.SetMtype(TypePush)
	output.SetUri(uri)
	output.SetBody(arg)

	for _,fn := range setting {
		fn(output)
	}

	if output.Seq() == 0 {
		s.seqLock.Lock()
		s.seq++
		output.SetSeq(s.seq)
		s.seqLock.Unlock()
	}

	if output.BodyCodec() == codec.NilCodecId {
		output.SetBodyCodec(s.transport.defaultBodyCodec)
	}

	if age := s.ContextAge(); age > 0 {
		ctxTimeout, _ := context.WithTimeout(output.Context(),age)
		socket.WithContext(ctxTimeout)(output)
	}

	defer func() {
		if p := recover(); p != nil {
			log.Printf("panic : %v\n",p)
		}
		s.transport.putContext(ctx,true)
	}()

	var(
		usedConn net.Conn
		err error
	)

W:
	if usedConn,err = s.write(output); err != nil {
		if err == errConnClosed && s.redialForClient(usedConn) {
			goto W
		}
		return err
	}

	return nil
}

////////////////////////////////////////////////////////////


type SessionHub struct {
	seesions sync.Map
}

func newSessionHub() *SessionHub {
	hub := &SessionHub{
		seesions: sync.Map{},
	}
	return hub
}

func (sh *SessionHub) Set(sess *session) {
	_sess,loaded := sh.seesions.LoadOrStore(sess.Id(),sess)
	if !loaded {
		return
	}
	sh.seesions.Store(sess.Id(),sess)
	if oldSess := _sess.(*session); sess!= oldSess {
		oldSess.Close()
	}

}

func (sh *SessionHub) Get(id string) (*session,bool) {
	_sess, ok := sh.seesions.Load(id)
	if !ok {
		return nil, false
	}
	return _sess.(*session),true

}

func (sh *SessionHub) Range(fn func(*session) bool) {
	sh.seesions.Range(func(key, value interface{}) bool {
		return fn(value.(*session))
	})
}

func (sh *SessionHub) Delete(id string) {
	sh.seesions.Delete(id)
}

