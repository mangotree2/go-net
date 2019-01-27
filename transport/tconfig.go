package transport

import (
	"errors"
	"github.com/henrylee2cn/teleport/socket"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net"
	"strconv"
	"time"
)

type TransportConfig struct {
	Network		string `yaml:"network"`
	LocalIP		string `yaml:"local_ip"`
	ListenPort	uint16	`yaml:"listen_port"`
	DefaultDialTimeout time.Duration `yaml:"default_dial_timeout"`
	RedialTimes int32 	`yaml:"redial_times"`
	DefaultBodyCodec string `yaml:"default"`
	DefaultSessionAge time.Duration `yaml:"default_session_age"`
	DefaultContextAge time.Duration `yaml:"default_context_age"`
	SlowComeTime	  time.Duration `yaml:"slow_come_time"`
	PrintDetail 		bool 	`yaml:"print_detail"`
	CountTime			bool 	`yaml:"cont_time"`

	localAddr 	net.Addr
	listenAddrStr string
	checked  bool

	//
	slowCometDuration time.Duration


}

func (t *TransportConfig) check() error {
	if t.checked {
		return nil
	}

	t.checked = true

	if len(t.LocalIP) == 0 {
		t.LocalIP = "0.0.0.0"
	}

	var err error

	switch t.Network {
	default:
		return errors.New("Invalid network config,now just follow tcp ,tcp4,tcp6")
	case "":
		t.Network = "tcp"
		fallthrough
	case "tcp","tcp4","tcp6":
		t.localAddr,err = net.ResolveTCPAddr(t.Network,net.JoinHostPort(t.LocalIP,"0"))

	}

	if err != nil {
		return err
	}

	t.listenAddrStr = net.JoinHostPort(t.LocalIP,strconv.FormatUint(uint64(t.ListenPort),10))

	if len(t.DefaultBodyCodec) == 0 {
		t.DefaultBodyCodec = "json"
	}

	if t.RedialTimes < 0 {
		t.RedialTimes = 0
	}

	//todo slow come time
	//t.slowCometDuration = math.MaxInt64
	//if t.SlowComeTime > 0 {
	//	t.slowCometDuration = t.SlowComeTime
	//}

	return nil
}

func (t *TransportConfig) LoadByLocalfile() error {
	cfgFile,err := ioutil.ReadFile("ts.yaml")
	if err != nil {
		return err
	}

	var c TransportConfig
	err = yaml.Unmarshal(cfgFile,&c)

	if err != nil {
		return err

	}

	return c.check()
}


var DefaultProtoFunc = socket.DefaultProtoFunc

var SetDefaultProtoFunc = socket.SetDefaultProtoFunc

var GetReadLimit = socket.MessageSizeLimit

var SetReadLimit = socket.SetMessageSizeLimit

var SetSocketKeepAlive = socket.SetKeepAlive

var SetSocketKeepAlivePeriod = socket.SetKeepAlivePeriod

var SetSocketReadBuffer = socket.SetReadBuffer

var SocketReadBuffer = socket.ReadBuffer

var SocketWriteBuffer = socket.WriteBuffer

var SetSocketWriteBuffer = socket.SetWriteBuffer

var SetSocketNodelay = socket.SetNoDelay

