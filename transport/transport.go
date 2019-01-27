package transport

import (
	"context"
	"errors"
	"fmt"
	"github.com/mangotree2/go-net/codec"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)


type (

	RouterTranSport interface {
		RouteCall(ctrlStruct interface{}) []string

	}

	TranSport interface {
		RouterTranSport
		ListenAndServe(protoFunc ...ProtoFunc) error
		Dial(addr string, protoFunc ...ProtoFunc) (Session,error)
		DialContext(ctx context.Context, addr string, protoFun ...ProtoFunc) (Session,error)
		ServeConn(conn net.Conn, protoFunc ...ProtoFunc) (Session,error)
		ServeListener(lis net.Listener, protoFunc ...ProtoFunc) error

	}


)

type transport struct {
	router *Router
	//tls
	sessHub *SessionHub
	closeCh	chan struct{}
	defaultSessionAge time.Duration
	defaultContextAge time.Duration
	slowComeTime	  time.Duration
	defaultBodyCodec	byte
	printDetail			bool
	countTime			bool
	timeNow 			func() time.Time
	timeSince			func(time.Time) time.Duration
	mu 					sync.Mutex

	network 		string

	//client
	defaultDialTimeout	time.Duration
	redialTimes			int32
	localAddr			net.Addr

	//server
	listenAddr 		string
	listeners       map[net.Listener]struct{}

}


func NewTranSport(cfg TransportConfig) TranSport {

	if err := cfg.check(); err != nil {
		log.Fatal("NewTranSport %v",err)
	}

	var t = &transport{
		router: 			newRouter("/"),
		sessHub:			newSessionHub(),
		closeCh:            make(chan struct{}),
		defaultSessionAge:  cfg.DefaultSessionAge,
		defaultContextAge:  cfg.DefaultContextAge,
		slowComeTime:       cfg.SlowComeTime,
		printDetail:        cfg.PrintDetail,
		countTime:          cfg.CountTime,
		mu:                 sync.Mutex{},
		network:            cfg.Network,
		defaultDialTimeout: cfg.DefaultDialTimeout,
		redialTimes:        cfg.RedialTimes,
		localAddr:          cfg.localAddr,
		listenAddr:         cfg.listenAddrStr,
		listeners:          make(map[net.Listener]struct{}),
	}

	if c,err := codec.GetByName(cfg.DefaultBodyCodec); err != nil {
		log.Fatalf("newTranSport codec %v",err)
	} else {
		t.defaultBodyCodec = c.Id()
	}

	if t.countTime {
		t.timeNow = time.Now
		t.timeSince = time.Since
	} else {
		t0 := time.Time{}
		t.timeNow = func() time.Time {
			return t0
		}
		t.timeSince = func(i time.Time) time.Duration {
			return 0
		}

	}

	//addTranSport


	return t
}

func (t *transport) getContext(s *session, withWg bool) *handlerCtx {
	if withWg {
		s.graceCtxWaitGroup.Add(1)
	}

	ctx := ctxPool.Get().(*handlerCtx)
	ctx.clean()
	ctx.reInit(s)
	return ctx
}

func (t *transport) putContext(ctx *handlerCtx, withWg bool) {
	if withWg {
		ctx.sess.graceCtxWaitGroup.Done()
	}

	ctxPool.Put(ctx)
}


func (t *transport) ListenAndServe(protoFunc ...ProtoFunc) error {
	if len(t.listenAddr) == 0 {
		log.Fatalf("listen addr is nil")
	}
	//todo 热重起
	laddr,err := net.ResolveTCPAddr(t.network,t.listenAddr)
	if err != nil {
		return err
	}
	lis,err := net.ListenTCP(t.network,laddr)
	if err != nil {
		log.Fatalf("listen and return server err :%v",err)
	}

	return t.ServeListener(lis,protoFunc...)


}

var ErrListenClosed = errors.New("listener is closed")

func (t *transport) ServeListener(lis net.Listener, protoFunc ...ProtoFunc) error {
	defer lis.Close()
	t.listeners[lis] = struct{}{}

	network := lis.Addr().Network()
	addr := lis.Addr().String()

	log.Printf("listen and serve network[%s], addr[%s]",network,addr)

	var (
		tempDelay time.Duration
		closeCh = t.closeCh
	)

	for {
		conn, e := lis.Accept()
		if e != nil {
			select {
			case <- closeCh:
				return ErrListenClosed
			default:
			}
			if ne ,ok := e.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2

				}


				if max := 1 * time.Second;tempDelay > max {
					tempDelay = max
				}

				log.Printf("accept err[%s]; retry at [%d]",e.Error(),tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return e

		}

		tempDelay = 0

		go func() {
			//
			if t.defaultSessionAge > 0 {
				conn.SetReadDeadline(time.Now().Add(t.defaultSessionAge))
			}

			if t.defaultContextAge > 0 {
				conn.SetReadDeadline(time.Now().Add(t.defaultContextAge))
			}
			//握手校验

			var sess = newSession(t,conn,protoFunc)
			//todo pulgin

			log.Printf("accept ok (network :%s, addr :%s, id:%s)",network,sess.RemoteAddr().String(),sess.Id())

			t.sessHub.Set(sess)
			sess.startReadAndHandle()



		}()

	}

}

func (t *transport) ServeConn(conn net.Conn, protoFunc ...ProtoFunc) (Session, error) {
	network := conn.LocalAddr().Network()
	if strings.Contains(network,"udp") || strings.Contains(network,"unix"){
		return nil, fmt.Errorf("invalid network : just follow tcp")
	}
	var sess = newSession(t, conn, protoFunc)
	t.sessHub.Set(sess)
	go sess.startReadAndHandle()
	return sess, nil
}

func (t *transport) DialContext(ctx context.Context, addr string, protoFun ...ProtoFunc) (Session,error) {
	return t.newSessionForClient(func() (net.Conn, error) {
		d := net.Dialer{
			LocalAddr:t.localAddr,
		}
		return d.DialContext(ctx,t.network,addr)
	},addr,protoFun)
}

func (t *transport)Dial(addr string, protoFunc ...ProtoFunc) (Session,error) {
	return t.newSessionForClient(func() (net.Conn, error) {
		d := net.Dialer{
			LocalAddr: t.localAddr,
			Timeout:   t.defaultDialTimeout,
		}
		return d.Dial(t.network,addr)

	},addr,protoFunc)

}

func (t *transport)newSessionForClient(dialFunc func()(net.Conn, error),addr string,protoFuncs []ProtoFunc) (*session,error) {
	var conn net.Conn
	var dialErr error

	for i:= t.redialTimes+1;i>0;i-- {
		conn,dialErr = dialFunc()
		if dialErr == nil {
			break
		}
	}

	if dialErr != nil {
		err := errDialFailed
		return nil, err
	}

	//tls

	var sess = newSession(t, conn, protoFuncs)

	if t.redialTimes > 0 {
		sess.redialForClientLocked = func(old net.Conn) bool {
			if old != sess.conn {
				return true
			}

			var err error
			for i := t.redialTimes; i>0;i-- {
				err = t.renewSessionForClient(sess,dialFunc,addr,protoFuncs)
				if err == nil {
					return true
				}

			}

			if err != nil {
				log.Printf("redial fial(network:%s, addr:%s, id:%s): %s", t.network, sess.RemoteAddr().String(), sess.Id(), err.Error())
			}
			return false

		}
	}

	sess.socket.SetId(sess.LocalAddr().String())

	//todo pulgin

	go sess.startReadAndHandle()

	t.sessHub.Set(sess)
	log.Printf("dial ok (network:%s, addr: %s, id:%s)",t.network,sess.LocalAddr().String(),sess.Id())
	return sess,nil
}

func (t *transport) renewSessionForClient(sess *session, dialfunc func() (net.Conn, error), addr string, protoFuncs []ProtoFunc) error {
	var conn, dialErr = dialfunc()

	if dialErr != nil {
		return  dialErr
	}

	//tls

	oldIp := sess.LocalAddr().String()
	oldId := sess.Id()
	if sess.conn != nil {
		sess.conn.Close()
	}

	sess.conn = conn
	sess.socket.Reset(conn,protoFuncs...)
	if oldIp == oldId {
		sess.socket.SetId(sess.LocalAddr().String())
	} else {
		sess.socket.SetId(oldId)
	}

	atomic.StoreInt32(&sess.status,statusOk)

	go sess.startReadAndHandle()

	t.sessHub.Set(sess)

	log.Printf("redial ok (addr :%s, id :%s)",sess.LocalAddr().String(),sess.Id())
	return nil

}

func (p *transport) RouteCall(callCtrlStruct interface{}) []string {
	return p.router.RouteCall(callCtrlStruct)
}