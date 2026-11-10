package control

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"sync"
	"time"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Token   string          `json:"token"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Handler func(context.Context, string, json.RawMessage) (any, *Error)

type Server struct {
	listener net.Listener
	token    string
	handler  Handler
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	closed   bool
	ctx      context.Context
	cancel   context.CancelFunc
	quit     func()
	quitOnce sync.Once
}

func Start(listener net.Listener, token string, handler Handler, quit func()) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{listener: listener, token: token, handler: handler, conns: make(map[net.Conn]struct{}), ctx: ctx, cancel: cancel, quit: quit}
	go server.accept()
	return server
}

func (server *Server) accept() {
	for {
		conn, err := server.listener.Accept()
		if err != nil {
			return
		}
		server.mu.Lock()
		if server.closed || len(server.conns) >= 64 {
			closed := server.closed
			server.mu.Unlock()
			conn.Close()
			if closed {
				return
			}
			continue
		}
		server.conns[conn] = struct{}{}
		server.mu.Unlock()
		go server.serve(conn)
	}
}

func (server *Server) serve(conn net.Conn) {
	defer func() {
		conn.Close()
		server.mu.Lock()
		delete(server.conns, conn)
		server.mu.Unlock()
	}()
	scanner := bufio.NewScanner(conn)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	encoder := json.NewEncoder(conn)
	for scanner.Scan() {
		var req Request
		if json.Unmarshal(scanner.Bytes(), &req) != nil || req.JSONRPC != "2.0" || req.Method == "" {
			if encoder.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &Error{Code: -32600, Message: "invalid request"}}) != nil {
				return
			}
			continue
		}
		if subtle.ConstantTimeCompare([]byte(req.Token), []byte(server.token)) != 1 {
			_ = encoder.Encode(response{JSONRPC: "2.0", ID: req.ID, Error: &Error{Code: -32001, Message: "unauthorized"}})
			return
		}
		_ = conn.SetReadDeadline(time.Time{})
		ctx, cancel := context.WithTimeout(server.ctx, 5*time.Second)
		result, rpcErr := server.handler(ctx, req.Method, req.Params)
		cancel()
		if len(req.ID) != 0 {
			if result == nil && rpcErr == nil {
				result = struct{}{}
			}
			if rpcErr != nil {
				result = nil
			}
			if encoder.Encode(response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}) != nil {
				return
			}
		}
		if req.Method == "instance.quit" && server.quit != nil && rpcErr == nil {
			server.quitOnce.Do(func() { go server.quit() })
			return
		}
	}
}

func (server *Server) Close() error {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.closed = true
	server.cancel()
	err := server.listener.Close()
	for conn := range server.conns {
		conn.Close()
	}
	return err
}
