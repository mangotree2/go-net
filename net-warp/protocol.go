package socket

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"sync"
)

type Proto interface {
	Version() (byte,string)
	Pack(*Message) error
	Unpack(*Message) error
}

type ProtoFunc func(io.ReadWriter) Proto

var defaultProtoFunc = NewRawProtoFunc

//原生协议
type rawProto struct {
	id   byte
	name string
	r    io.Reader
	w    io.Writer
	rMu  sync.Mutex
}

var NewRawProtoFunc = func(rw io.ReadWriter) Proto {
	return &rawProto{
		id:   'r',
		name: "raw",
		r:    rw,
		w:    rw,
	}
}

func (r *rawProto) Version() (byte, string) {
	return r.id, r.name
}

//写入消息
func (r *rawProto) Pack(m *Message) error {
	//var all = make([]byte, m.Size()+4)
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, uint32(0))
	if err != nil {
		log.Println("Pack binary",err)
		return err
	}

	err = buf.WriteByte(r.id)
	if err != nil {
		log.Println("Pack WriteByte",err)

		return err
	}

	//之后可以做压缩或者加密处理
	//prefixLen := buf.Len()

	//header
	err  = r.writeHeader(buf,m)
	if err != nil {
		log.Println("Pack writeHeader",err)

		return err
	}

	//body
	err = r.writeBody(buf,m)
	if err != nil {
		log.Println("Pack writeBody",err)

		return err
	}

	//header 和body 加密
	log.Println("Pack len “",buf.Len())
	err = m.SetSize(uint32(buf.Len()))
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint32(buf.Bytes(),m.Size())

	_, err = r.w.Write(buf.Bytes())
	if err != nil {
		return err
	}

	return err

}

func (r *rawProto) Unpack(m *Message) error {
	buf := make([]byte,1024)
	err := r.readMessage(buf,m)
	if err != nil {
		log.Println("Unpack readMessage err")
		return err
	}

	log.Println(buf)
	buf = r.readHeader(buf,m)

	return r.readBody(buf,m)

}


var errProtoUnmatch = errors.New("mismatched protocol")

func (r *rawProto) readMessage(buf []byte,m *Message) error {
	r.rMu.Lock()
	defer r.rMu.Unlock()

	if len(buf) < 1 {
		buf = make([]byte,1024)
	}

	var size uint32
	err := binary.Read(r.r,binary.BigEndian,&size)
	if err != nil {
		return err
	}

	if err = m.SetSize(size);err != nil {
		return err
	}

	_, err = io.ReadFull(r.r, buf[0:1])
	if err != nil {
		return err
	}

	if buf[0] != r.id {
		return errProtoUnmatch
	}


	//解密操作
	//...


	var lastLen = int(size) - 4 - 1
	buf = make([]byte,lastLen)
	_,err =io.ReadFull(r.r,buf)
	return err
}

func (r *rawProto) readHeader(buf []byte,m *Message) []byte {
	seq := binary.BigEndian.Uint32(buf)
	m.SetSeq(int32(seq))

	buf = buf[4:]
	m.SetMtype(buf[0])

	buf = buf[1:]

	uriLen := binary.BigEndian.Uint16(buf)
	buf = buf[2:]
	m.SetUri(string(buf[:uriLen]))

	buf = buf[uriLen:]
	metaLen := binary.BigEndian.Uint16(buf)

	buf = buf[2:]
	m.meta.UnMarshal(buf[:metaLen])

	buf = buf[metaLen:]
	return buf
}

func (r *rawProto) readBody(data []byte, m *Message) error {
	m.SetBodyCodec(data[0])
	return m.UnmarshalBody(data[1:])
}


func (r *rawProto) writeHeader(buf *bytes.Buffer,m *Message) error {

	//seq
	err := binary.Write(buf,binary.BigEndian,m.Seq())
	if err != nil {
		log.Println("writeHeader Seq: ",err)
		return err
	}

	//mtype
	err = buf.WriteByte(m.Mtype())
	if err != nil {
		log.Println("writeHeader Mtype: ",err)

		return err
	}

	//uri
	err = binary.Write(buf,binary.BigEndian,uint16(len(m.Uri())))
	if err != nil {
		log.Println("writeHeader lenUri: ",err)

		return err
	}
	err = binary.Write(buf,binary.BigEndian,[]byte(m.Uri()))
	if err != nil {
		log.Println("writeHeader m Uri: ",err)

		return err
	}

	//meta
	metaBytes,err := m.Meta().Marshal()
	if err != nil {
		log.Println("writeHeader Meta Marshal: ",err)

		return err
	}

	err = binary.Write(buf, binary.BigEndian, uint16(len(metaBytes)))
	if err != nil {
		log.Println("writeHeader len metaBytes: ",err)

		return err
	}

	_,err = buf.Write(metaBytes)

	if err != nil {
		log.Println("writeHeader metaBytes: ",err)

		return err
	}
	return nil

}

func (r *rawProto) writeBody(buf *bytes.Buffer,m *Message) error {
	err := buf.WriteByte(m.BodyCodec())
	if err != nil {
		return err
	}

	bodyBytes,err := m.MarshalBody()
	if err != nil {
		return err
	}

	_,err = buf.Write(bodyBytes)
	if err != nil {
		return err
	}

	return nil
}





