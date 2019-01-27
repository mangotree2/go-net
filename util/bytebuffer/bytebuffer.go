package byteBuffer

import (
	"io"
)

type ByteBuffer struct {
	B []byte
}


func (b *ByteBuffer) Len() int {
	return len(b.B)
}

func (b *ByteBuffer)ReadFrom(r io.Reader) (int64,error) {
	p := b.B

	nStart := int64(len(p))
	nMax := int64(cap(p))

	n := nStart

	if nMax == 0 {
		nMax = 64
		p = make([]byte,nMax)
	} else {
		p = p[:nMax]
	}

	for {
		if n == nMax {
			nMax = 2 * nMax
			bNew := make([]byte,nMax)
			copy(bNew,p)
			p = bNew
		}

		nn, err := r.Read(p[n:])
		n += int64(nn)
		if err != nil {
			b.B = p[:n]
			n -= nStart
			if err == io.EOF {
				return n,nil
			}
			return n, err
		}
	}

}

func (b *ByteBuffer) Bytes() []byte {
	return b.B
}

