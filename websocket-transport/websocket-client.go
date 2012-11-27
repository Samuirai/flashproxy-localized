package main

import (
	"code.google.com/p/go.net/websocket"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"time"
)

const socksTimeout = 2
const bufSiz = 1500

// When a connection handler starts, +1 is written to this channel; when it
// ends, -1 is written.
var handlerChan = make(chan int)

func logDebug(format string, v ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", v...)
}

func proxy(local *net.TCPConn, ws *websocket.Conn) {
	var wg sync.WaitGroup

	wg.Add(2)

	// Local-to-WebSocket read loop.
	go func() {
		buf := make([]byte, bufSiz)
		var err error
		for {
			n, er := local.Read(buf[:])
			if n > 0 {
				ew := websocket.Message.Send(ws, buf[:n])
				if ew != nil {
					err = ew
					break
				}
			}
			if er != nil {
				err = er
				break
			}
		}
		if err != nil && err != io.EOF {
			logDebug("%s", err)
		}
		local.CloseRead()
		ws.Close()

		wg.Done()
	}()

	// WebSocket-to-local read loop.
	go func() {
		var buf []byte
		var err error
		for {
			er := websocket.Message.Receive(ws, &buf)
			if er != nil {
				err = er
				break
			}
			n, ew := local.Write(buf)
			if ew != nil {
				err = ew
				break
			}
			if n != len(buf) {
				err = io.ErrShortWrite
				break
			}
		}
		if err != nil && err != io.EOF {
			logDebug("%s", err)
		}
		local.CloseWrite()
		ws.Close()

		wg.Done()
	}()

	wg.Wait()
}

func handleConnection(conn *net.TCPConn) error {
	defer conn.Close()

	handlerChan <- 1
	defer func() {
		handlerChan <- -1
	}()

	conn.SetDeadline(time.Now().Add(socksTimeout * time.Second))
	dest, err := ReadSocks4aConnect(conn)
	if err != nil {
		SendSocks4aResponseFailed(conn)
		return err
	}
	// Disable deadline.
	conn.SetDeadline(time.Time{})
	logDebug("SOCKS request for %s", dest)

	// We need the parsed IP and port for the SOCKS reply.
	destAddr, err := net.ResolveTCPAddr("tcp", dest)
	if err != nil {
		SendSocks4aResponseFailed(conn)
		return err
	}

	wsUrl := url.URL{Scheme: "ws", Host: dest}
	ws, err := websocket.Dial(wsUrl.String(), "", wsUrl.String())
	if err != nil {
		SendSocks4aResponseFailed(conn)
		return err
	}
	defer ws.Close()
	logDebug("WebSocket connection to %s", ws.Config().Location.String())

	SendSocks4aResponseGranted(conn, destAddr)

	proxy(conn, ws)

	return nil
}

func socksAcceptLoop(ln *net.TCPListener) error {
	for {
		socks, err := ln.AcceptTCP()
		if err != nil {
			return err
		}
		go func() {
			err := handleConnection(socks)
			if err != nil {
				logDebug("SOCKS from %s: %s", socks.RemoteAddr(), err)
			}
		}()
	}
	return nil
}

func startListener(addrStr string) (*net.TCPListener, error) {
	addr, err := net.ResolveTCPAddr("tcp", addrStr)
	if err != nil {
		return nil, err
	}
	ln, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return nil, err
	}
	go func() {
		err := socksAcceptLoop(ln)
		if err != nil {
			logDebug("accept: %s", err)
		}
	}()
	return ln, nil
}

func main() {
	const ptMethodName = "websocket"
	var defaultSocksAddrStrs = []string{"127.0.0.1:0", "[::1]:0"}
	var socksAddrStrs []string

	var socksArg = flag.String("socks", "", "address on which to listen for SOCKS connections")
	flag.Parse()
	if *socksArg != "" {
		socksAddrStrs = []string{*socksArg}
	} else {
		socksAddrStrs = defaultSocksAddrStrs
	}

	PtClientSetup([]string{ptMethodName})

	listeners := make([]*net.TCPListener, 0)
	for _, socksAddrStr := range socksAddrStrs {
		ln, err := startListener(socksAddrStr)
		if err != nil {
			PtCmethodError(ptMethodName, err.Error())
		}
		PtCmethod(ptMethodName, "socks4", ln.Addr())
		listeners = append(listeners, ln)
	}
	PtCmethodsDone()

	var numHandlers int = 0

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	var sigint bool = false
	for !sigint {
		select {
		case n := <-handlerChan:
			numHandlers += n
		case <-signalChan:
			logDebug("SIGINT")
			sigint = true
		}
	}

	for _, ln := range listeners {
		ln.Close()
	}

	sigint = false
	for numHandlers != 0 && !sigint {
		select {
		case n := <-handlerChan:
			numHandlers += n
		case <-signalChan:
			logDebug("SIGINT")
			sigint = true
		}
	}
}
