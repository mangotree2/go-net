package codec

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
)

const (

	NAME_PROTOBUF = "protobuf"
	ID_PROTOBUF = 'p'
)

func init() {
	Reg(new(ProtoCodec))
}

type ProtoCodec struct {}


func (ProtoCodec) Name() string {
	return NAME_PROTOBUF
}


func (ProtoCodec) Id() byte {
	return ID_PROTOBUF
}

func (ProtoCodec) Marshal(v interface{}) ([]byte, error) {
	return ProtoMarshal(v)
}

func (ProtoCodec) UnMarshal(data []byte,v interface{}) error {
	return ProtoUnMarshal(data,v)
}


var (
	EmptyStruct = new(PbEmpty)
)

func ProtoMarshal(v interface{}) ([]byte,error) {
	if p, ok := v.(proto.Message); ok {
		return proto.Marshal(p)
	}

	switch v.(type) {
	case nil, *struct{},struct{}:
		return proto.Marshal(EmptyStruct)
	}

	return nil,fmt.Errorf("protobuf %T not implement proto.Message",v)

}

func ProtoUnMarshal(data []byte,v interface{}) error {
	if p,ok := v.(proto.Message); ok {
		return proto.Unmarshal(data,p)
	}

	switch v.(type) {
	case nil, *struct{}, struct{}:
		return nil
	}
	return fmt.Errorf("protobuf %T not implement proto.Message",v)
}

