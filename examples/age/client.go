package main

import (
	"context"
	"github.com/mangotree2/go-net/transport"
	"log"
	"time"

)

//go:generate go build $GOFILE

func main() {
	log.SetFlags(log.Lshortfile)

	cli := transport.NewTranSport(transport.TransportConfig{PrintDetail: true})
	sess, err := cli.Dial(":9090")
	if err != nil {
		log.Fatalf("%v", err)
	}

	var result string
	sess.Call("/test/ok", "test1", &result)
	log.Printf("test sync1: %v", result)
	result = ""

	log.Println()
	rerr := sess.Call("/test/timeout", "test2", &result).Error()
	log.Printf("test sync2: server context timeout: %v", rerr)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)

	result = ""
	log.Println()

	callCmd := sess.AsyncCall(
		"/test/timeout",
		"test3",
		&result,
		make(chan transport.CallCmd, 1),
		transport.WithContext(ctx),
	)
	select {
	case <-callCmd.Done():
		cancel()
		log.Printf("test async1: %v", result)
	case <-ctx.Done():
		log.Printf("test async1: client context timeout: %v", ctx.Err())
	}

	time.Sleep(time.Second * 6)
	result = ""

	log.Println()


	rerr = sess.Call("/test/ok", "test4", &result).Error()
	log.Printf("test sync3: disconnect due to server session timeout: %v", rerr)

	sess, err = cli.Dial(":9090")
	if err != nil {
		log.Fatalf("%v", err)
	}
	sess.AsyncCall(
		"/test/break",
		nil,
		nil,
		make(chan transport.CallCmd, 1),
	)
}
