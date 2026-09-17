package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

var errTestServe = errors.New("test serve failure")

type blockingListener struct {
	accepted chan struct{}
	closed   chan struct{}
	once     sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{
		accepted: make(chan struct{}),
		closed:   make(chan struct{}),
	}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	select {
	case <-l.accepted:
	default:
		close(l.accepted)
	}
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *blockingListener) Addr() net.Addr { return testAddr("blocking") }

type failingListener struct{}

func (failingListener) Accept() (net.Conn, error) { return nil, errTestServe }
func (failingListener) Close() error              { return nil }
func (failingListener) Addr() net.Addr            { return testAddr("failing") }

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }

func TestServeServersStopsHTTPAndMCPOnContextCancellation(t *testing.T) {
	httpListener := newBlockingListener()
	mcpListener := newBlockingListener()
	httpServer := &http.Server{Handler: http.NewServeMux()}
	mcpServer := &http.Server{Handler: http.NewServeMux()}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveServers(ctx, httpServer, httpListener, mcpServer, mcpListener)
	}()

	select {
	case <-httpListener.accepted:
	case <-time.After(time.Second):
		t.Fatal("HTTP server did not start accepting")
	}
	select {
	case <-mcpListener.accepted:
	case <-time.After(time.Second):
		t.Fatal("MCP server did not start accepting")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveServers returned shutdown error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveServers did not stop after context cancellation")
	}
}

func TestRunServerLifecyclePropagatesUnexpectedServeError(t *testing.T) {
	server := &http.Server{Handler: http.NewServeMux()}
	err := runServerLifecycle(context.Background(), server, failingListener{}, nil, nil)
	if !errors.Is(err, errTestServe) {
		t.Fatalf("expected serve error %v, got %v", errTestServe, err)
	}
}
