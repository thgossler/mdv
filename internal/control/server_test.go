package control

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestServerAuthenticationAndRequests(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := Start(listener, "secret", func(_ context.Context, method string, params json.RawMessage) (any, *Error) {
		return map[string]string{"method": method}, nil
	}, nil)
	defer server.Close()
	conn, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	encoder := json.NewEncoder(conn)
	reader := bufio.NewReader(conn)
	for _, req := range []Request{{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "info", Token: "secret"}, {JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "info", Token: "wrong"}} {
		if err := encoder.Encode(req); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var got response
		if err := json.Unmarshal(line, &got); err != nil {
			t.Fatal(err)
		}
		if (req.Token == "secret") != (got.Error == nil) {
			t.Fatalf("unexpected response: %s", line)
		}
	}
}

func TestInstanceDiscoveryAndCleanup(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	instance, err := StartInstance("tui", func(context.Context, string, json.RawMessage) (any, *Error) { return nil, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(instance.Path) != filepath.Join(base, "mdv", "api") {
		t.Fatalf("unexpected discovery path %s", instance.Path)
	}
	info, err := os.Stat(instance.Path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("insecure permissions: %v", info.Mode())
	}
	data, err := os.ReadFile(instance.Path)
	if err != nil {
		t.Fatal(err)
	}
	var record struct{ Address, Token, Mode string }
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Token == "" || record.Mode != "tui" || record.Address == "" {
		t.Fatalf("invalid discovery record: %#v", record)
	}
	instance.Close()
	if _, err := os.Stat(instance.Path); !os.IsNotExist(err) {
		t.Fatalf("record not removed: %v", err)
	}
}

func TestErrorResponseOmitsResult(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := Start(listener, "secret", func(context.Context, string, json.RawMessage) (any, *Error) {
		return json.RawMessage("null"), &Error{Code: -32002, Message: "stale"}
	}, nil)
	defer server.Close()
	conn, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"test\",\"token\":\"secret\"}\n")); err != nil {
		t.Fatal(err)
	}
	var response map[string]json.RawMessage
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if _, exists := response["result"]; exists {
		t.Fatal("error response contains result")
	}
	if _, exists := response["error"]; !exists {
		t.Fatal("missing error")
	}
}

func TestQuitRespondsBeforeClosing(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	quit := make(chan struct{})
	var server *Server
	server = Start(listener, "secret", func(context.Context, string, json.RawMessage) (any, *Error) {
		return map[string]bool{"quitting": true}, nil
	}, func() { server.Close(); close(quit) })
	defer server.Close()
	conn, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"instance.quit\",\"token\":\"secret\"}\n")); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Result struct {
			Quitting bool `json:"quitting"`
		} `json:"result"`
		Error *Error `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&got); err != nil {
		t.Fatalf("missing quit acknowledgement: %v", err)
	}
	if got.Error != nil || !got.Result.Quitting {
		t.Fatalf("unexpected response: %#v", got)
	}
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("quit callback not called")
	}
}

func TestQuitRequiresAuthenticationAndSuccessfulHandler(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	quit := make(chan struct{}, 1)
	server := Start(listener, "secret", func(context.Context, string, json.RawMessage) (any, *Error) {
		return nil, &Error{Code: -32000, Message: "UI not ready"}
	}, func() { quit <- struct{}{} })
	defer server.Close()
	for _, token := range []string{"wrong", "secret"} {
		conn, err := net.Dial("tcp4", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(conn).Encode(Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "instance.quit", Token: token}); err != nil {
			t.Fatal(err)
		}
		var got response
		if err := json.NewDecoder(conn).Decode(&got); err != nil {
			t.Fatal(err)
		}
		conn.Close()
		if got.Error == nil {
			t.Fatalf("quit unexpectedly accepted for token %q", token)
		}
	}
	select {
	case <-quit:
		t.Fatal("rejected quit shut down the instance")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestQuitNotification(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	quit := make(chan struct{})
	server := Start(listener, "secret", func(context.Context, string, json.RawMessage) (any, *Error) {
		return map[string]bool{"quitting": true}, nil
	}, func() { close(quit) })
	defer server.Close()
	conn, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(Request{JSONRPC: "2.0", Method: "instance.quit", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("quit notification did not trigger shutdown")
	}
	if _, err := bufio.NewReader(conn).ReadByte(); err != io.EOF {
		t.Fatalf("notification unexpectedly received a response: %v", err)
	}
}
