package meta

import "testing"

func TestMeta_Marshal(t *testing.T) {

	meta := NewMeta()

	//meta["context"] = "my test"
	//meta["a"]  = "b"
	meta.Add("context","my test")
	meta.Add("a","b")

	m := Meta(meta)

	b,err := m.Marshal()
	if err != nil{
		t.Error(err)
	}

	t.Log(string(b))

	m1 := NewMeta()
	e := m1.UnMarshal(b)
	if e != nil {
		t.Error(e)
	}


	m1.VisitAll(func(key, value string) {
		t.Log(key,value)
	})
}