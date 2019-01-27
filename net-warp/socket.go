package socket

import (
	"errors"
	"net"
	"time"
	"bufio"
	"sync"
	"io"
	"sync/atomic"
)

type Socket interface {
	// LocalAddr返回本地网络地址。
	LocalAddr() net.Addr
	// RemoteAddr返回远程网络地址。
	RemoteAddr() net.Addr
	//SetDeadline设置读写操作绝对期限，
	SetDeadline(t time.Time) error
	//SetReadDeadline设置读操作绝对期限
	SetReadDeadline(t time.Time) error
	//
	SetWriteDeadline(t time.Time) error
	//写入一条消息
	WriteMessage(message *Message) error
	//读出一条消息
	ReadMessage(message *Message) error
	//Read从连接中读取数据，SetReadDeadline后会返回超时错误
	Read(b []byte) (n int, err error)
	//
	Write(b []byte) (n int, err error)
	//
	Close() error
	//
	Id() string
	//
	SetId(string)

	Reset(netConn net.Conn, protoFunc ...ProtoFunc)

	Swap() *sync.Map

	//SwapLen() int

}

var readerSize = 1024

type socket struct {
	net.Conn
	readerWithBuffer	*bufio.Reader
	protocol         Proto
	id               string
	idMutex          sync.RWMutex
	mu               sync.RWMutex
	swap 			 sync.Map
	curState         int32
	fromPool         bool
}

const (
	normal      int32 = 0
	activeClose int32 = 1
)

//var (
//	_ net.Conn = Socket(nil)
//	_ RawConn  = (*socket)(nil)
//)
var ErrProactivelyCloseSocket = errors.New("socket is closed proactively")

var socketPool = sync.Pool{
	New : func() interface{} {
		s := newSocket(nil,nil)
		s.fromPool = true
		return s
	},
}


func GetSocket(c net.Conn, protoFunc ...ProtoFunc) Socket {
	s := socketPool.Get().(*socket)
	s.Reset(c, protoFunc...)
	return s
}

func NewSocket(c net.Conn, protoFunc ...ProtoFunc) Socket {
	return newSocket(c,protoFunc)
}

func newSocket(c net.Conn, ProtoFuncs []ProtoFunc) *socket {
	s := &socket{
		Conn:c,
		readerWithBuffer:bufio.NewReaderSize(c,readerSize),
	}

	s.protocol = getProto(ProtoFuncs,s)
	s.optimize()
	return s
}


func getProto(protoFuncs []ProtoFunc, rw io.ReadWriter) Proto {
	if len(protoFuncs) > 0 && protoFuncs[0] != nil {
		return protoFuncs[0](rw)
	}

	return defaultProtoFunc(rw)
}

func (s *socket) Raw() net.Conn {
	return s.Conn
}

func (s *socket) Read(b []byte) (int,error) {
	return s.readerWithBuffer.Read(b)
}

func (s *socket) WriteMessage(m *Message) error {
	s.mu.RLock()
	protocol := s.protocol
	s.mu.RUnlock()
	err := protocol.Pack(m)
	if err != nil && s.isActiveClosed() {
		err = ErrProactivelyCloseSocket
	}

	return err
}

func (s *socket) ReadMessage(m *Message) error {
	s.mu.RLock()
	protocol := s.protocol
	s.mu.RUnlock()
	return protocol.Unpack(m)
}

func (s *socket) Id() string {
	s.idMutex.RLock()
	id := s.id
	if len(id) == 0 {
		id = s.RemoteAddr().String()
	}
	s.idMutex.RUnlock()
	return id
}

func (s *socket) Close() error {
	if s.isActiveClosed() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isActiveClosed() {
		return nil
	}

	atomic.StoreInt32(&s.curState,activeClose)

	var err error
	if s.Conn != nil {
		err =s.Conn.Close()
	}

	if s.fromPool {
		s.Conn = nil
		s.protocol = nil
		socketPool.Put(s)
	}

	return err





}

func (s *socket) isActiveClosed() bool {
	return atomic.LoadInt32(&s.curState) == activeClose
}


func (s *socket) Reset(netConn net.Conn, protoFunc ...ProtoFunc) {
	atomic.StoreInt32(&s.curState, activeClose)
	s.mu.Lock()
	s.Conn = netConn
	s.readerWithBuffer.Discard(s.readerWithBuffer.Buffered())
	s.readerWithBuffer.Reset(netConn)
	s.protocol = getProto(protoFunc, s)
	s.SetId("")
	atomic.StoreInt32(&s.curState, normal)
	s.optimize()
	s.mu.Unlock()
}


func (s *socket) SetId(id string) {
	s.idMutex.Lock()
	s.id = id
	s.idMutex.Unlock()
}

func (s *socket)Swap() *sync.Map  {
	return &s.swap
}

//func (s *socket) SwapLen() int {
//	if s.swap == nil {
//		return 0
//	}
//	return len(s.swap.)
//}



func (s *socket) optimize() {
	if c, ok := s.Conn.(ifaceSetKeepAlive); ok {
		if changeKeepAlive {
			c.SetKeepAlive(keepAlive)
		}
		if keepAlivePeriod >= 0 && keepAlive {
			c.SetKeepAlivePeriod(keepAlivePeriod)

		}
	}

	if c, ok := s.Conn.(ifaceSetBuffer); ok {
		if readBuffer >= 0 {
			c.SetReadBuffer(readBuffer)
		}
		if writeBuffer >= 0 {
			c.SetWriteBuffer(writeBuffer)
		}
	}

	if c, ok := s.Conn.(ifaceSetNoDelay) ; ok {
		if !noDelay {
			c.SetNoDelay(noDelay)
		}
	}
}

var (
	writeBuffer     int           = -1
	readBuffer      int           = -1
	changeKeepAlive bool          = false
	keepAlive       bool          = true
	keepAlivePeriod time.Duration = -1
	noDelay         bool          = true
)

type (
	ifaceSetKeepAlive interface {
		//SetKeepAlive设置操作系统是否应该在该连接中发送keepalive信息
		SetKeepAlive(keepalive bool) error
		//SetKeepAlivePeriod设置keepalive的周期，超出会断开
		SetKeepAlivePeriod(d time.Duration) error
	}
	ifaceSetBuffer interface {
		//设置与连接关联的操作系统接收缓冲区的大小。
		SetReadBuffer(bytes int) error
		// SetWriteBuffer设置与连接关联的操作系统传输缓冲区的大小。
		SetWriteBuffer(bytes int) error
	}
	ifaceSetNoDelay interface {
		// SetNoDelay控制操作系统是否应该延迟
		//消息传输希望发送更少的消息（Nagle的
		//算法）。 默认值为true（无延迟），表示数据为
		//写入后尽快发送。
		SetNoDelay(noDelay bool) error
	}
)