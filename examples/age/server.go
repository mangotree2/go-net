package main

import (
	"context"
	"errors"
	"github.com/mangotree2/go-net/transport"
	"log"
	"time"
)

//go:generate go build $GOFILE

func main() {
	log.SetFlags(log.Lshortfile)
	srv := transport.NewTranSport(transport.TransportConfig{
		PrintDetail:       true,
		CountTime:         true,
		ListenPort:        9090,
		DefaultSessionAge: time.Second * 7,
		DefaultContextAge: time.Second * 2,
	})
	srv.RouteCall(new(test))
	srv.ListenAndServe()
}

type test struct {
	transport.CallCtx
}

func (t *test) Ok(arg *string) (string, error) {
	return *arg + " -> OK", nil
}

func (t *test) Timeout(arg *string) (string, error) {
	tCtx, _ := context.WithTimeout(t.Context(), time.Second)
	time.Sleep(time.Second)
	select {
	case <-tCtx.Done():
		return "", errors.New(
			tCtx.Err().Error(),
		)
	default:
		return *arg + " -> Not Timeout", nil
	}
}

func (t *test) Break(*struct{}) (*struct{}, error) {
	time.Sleep(time.Second * 3)
	select {
	case <-t.Session().CloseNotify():
		log.Println("the connection has gone away!")
		return nil, errors.New("xxx")
	default:
		return nil, nil
	}
}
