package codec


import (
	"encoding/json"
)

// json codec name and id
const (
	NAME_JSON = "json"
	ID_JSON   = 'j'
)

func init() {
	Reg(new(JsonCodec))
}

// JsonCodec json codec
type JsonCodec struct{}

// Name returns codec name.
func (JsonCodec) Name() string {
	return NAME_JSON
}

// Id returns codec id.
func (JsonCodec) Id() byte {
	return ID_JSON
}

// Marshal returns the JSON encoding of v.
func (JsonCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal parses the JSON-encoded data and stores the result
// in the value pointed to by v.
func (JsonCodec) UnMarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
