package codec

import "fmt"

type Codec interface {
	Id() byte
	Name() string
	Marshal(v interface{}) ([]byte,error)
	UnMarshal(data []byte,v interface{}) error

}

var codecMap = struct {
	idMap map[byte]Codec
	nameMap map[string]Codec
}{
	idMap:make(map[byte]Codec),
	nameMap:make(map[string]Codec),

}

const (
	NilCodecId byte = 0

	NilCodeName string = ""
)

func Reg(codec Codec) {
	if codec.Id() == NilCodecId {
		panic(fmt.Sprintf("Reg codec id can not be 0"))
	}

	if _,ok := codecMap.idMap[codec.Id()];ok {
		panic(fmt.Sprintf("Reg repeat coded id :%d",codec.Id()))
	}

	if _,ok := codecMap.nameMap[codec.Name()];ok {
		panic("Reg repeat codec name: " + codec.Name())

	}

	codecMap.idMap[codec.Id()] = codec
	codecMap.nameMap[codec.Name()] = codec

}


func Get(codeId byte) (Codec,error) {
	codec ,ok := codecMap.idMap[codeId]
	if !ok {
		return nil, fmt.Errorf("Not Found")
	}
	return codec,nil

}

func GetByName(codeName string) (Codec,error)  {
	codec, ok := codecMap.nameMap[codeName]
	if !ok {
		return nil, fmt.Errorf("Not Found")
	}

	return codec, nil
}

func Marshal(codecId byte, v interface{}) ([]byte, error) {
	codec, err := Get(codecId)
	if err != nil {
		return nil, err
	}

	return codec.Marshal(v)
}

func UnMarshal(codecId byte, data []byte, v interface{}) error {
	codec, err := Get(codecId)
	if err != nil {
		return err
	}

	return codec.UnMarshal(data,v)
}

func MarshalByName(codecName string, v interface{}) ([]byte,error) {
	codec, err := GetByName(codecName)
	if err != nil {
		return nil, err
	}

	return codec.Marshal(v)

}

func UnMarshalByName(codecName string, data []byte, v interface{}) error {
	codec, err := GetByName(codecName)
	if err != nil {
		return err
	}

	return codec.UnMarshal(data,v)
}

























