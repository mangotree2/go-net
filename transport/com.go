package transport

import (
	"errors"
	"github.com/mangotree2/go-net/codec"
	"github.com/mangotree2/go-net/net-warp"
	"github.com/mangotree2/go-net/util/meta"
	"strconv"
)

type ProtoFunc = socket.ProtoFunc
type MessageSetting = socket.MessageSetting
type Message = socket.Message
type Header = socket.Header
var WithContext = socket.WithContext

const (
	TypeUndefined byte = 0
	TypeCall      byte = 1
	TypeReply     byte = 2 // reply to call
	TypePush      byte = 3
)

const (
	MetaAcceptBodyCodec = "X-Accept-Body-Codec"

)

var (
	errCodeMtypeNotAllowed = errors.New("Mtype not allowed")
	errBadMessage = errors.New("bad Message can't not read")
	errInternalServerErr = errors.New("internal server err")
	errNotFound = errors.New("not found handle")
	errConnClosed = errors.New("conn is closed")
	errDialFailed = errors.New("conn dial failed")
)

func GetAcceptBodyCodec(meta meta.Meta) (byte, bool) {
	s := meta.Peek(MetaAcceptBodyCodec)
	if len(s) == 0 || len(s) > 3 {
		return 0, false
	}
	b, err := strconv.ParseUint(string(s), 10, 8)
	if err != nil {
		return 0, false
	}
	c := byte(b)
	return c, c != codec.NilCodecId
}