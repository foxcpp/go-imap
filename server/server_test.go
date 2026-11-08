package server_test

import (
	"bufio"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/backend"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
)

// Extnesions that are always advertised by go-imap server.
const builtinExtensions = "LITERAL+ SASL-IR CHILDREN UNSELECT MOVE IDLE APPENDLIMIT"

func testServer(t *testing.T) (s *server.Server, conn net.Conn) {
	bkd := memory.New()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("Cannot listen:", err)
	}

	s = server.New(bkd)
	s.AllowInsecureAuth = true

	go s.Serve(l)
	t.Cleanup(func() { s.Close() })

	conn, err = net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal("Cannot connect to server:", err)
	}

	return
}

func TestServer_greeting(t *testing.T) {
	s, conn := testServer(t)
	defer s.Close()
	defer conn.Close()

	scanner := bufio.NewScanner(conn)

	scanner.Scan() // Wait for greeting
	greeting := scanner.Text()

	if greeting != "* OK [CAPABILITY IMAP4rev1 "+builtinExtensions+" AUTH=PLAIN] IMAP4rev1 Service Ready" {
		t.Fatal("Bad greeting:", greeting)
	}
}

type blockingSelectExtension struct {
	started chan struct{}
	release chan struct{}
	mailbox backend.Mailbox
}

func (e *blockingSelectExtension) Capabilities(server.Conn) []string { return nil }

func (e *blockingSelectExtension) Command(name string) server.HandlerFactory {
	if name != "SELECT" {
		return nil
	}
	return func() server.Handler { return e }
}

func (e *blockingSelectExtension) Parse([]interface{}) error { return nil }

func (e *blockingSelectExtension) Handle(conn server.Conn) error {
	conn.Context().Mailbox = e.mailbox
	close(e.started)
	<-e.release
	return nil
}

type closeTrackingMailbox struct {
	backend.Mailbox
	closed chan struct{}
}

type closeServerExtension struct {
	closed chan error
}

func (*closeServerExtension) Capabilities(server.Conn) []string { return nil }

func (e *closeServerExtension) Command(name string) server.HandlerFactory {
	if name != "SELECT" {
		return nil
	}
	return func() server.Handler { return e }
}

func (*closeServerExtension) Parse([]interface{}) error { return nil }

func (e *closeServerExtension) Handle(conn server.Conn) error {
	e.closed <- conn.Server().Close()
	return nil
}

func TestServer_CloseFromCommandHandlerDoesNotDeadlock(t *testing.T) {
	ext := &closeServerExtension{closed: make(chan error, 1)}
	s := server.New(memory.New())
	s.Enable(ext)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("Cannot listen:", err)
	}
	go s.Serve(l)

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal("Cannot connect to server:", err)
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("Did not receive greeting:", scanner.Err())
	}
	if _, err := conn.Write([]byte("a001 SELECT INBOX\r\n")); err != nil {
		t.Fatal("Cannot send SELECT:", err)
	}

	select {
	case err := <-ext.closed:
		if err != nil {
			t.Fatal("Cannot close server:", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Server.Close deadlocked when called from a command handler")
	}
}

func (m *closeTrackingMailbox) Close() error {
	close(m.closed)
	return nil
}

func TestServer_CloseDuringCommandDefersMailboxClose(t *testing.T) {
	ext := &blockingSelectExtension{
		started: make(chan struct{}),
		release: make(chan struct{}),
		mailbox: &closeTrackingMailbox{closed: make(chan struct{})},
	}
	s := server.New(memory.New())
	s.Enable(ext)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("Cannot listen:", err)
	}
	go s.Serve(l)
	t.Cleanup(func() {
		select {
		case <-ext.release:
		default:
			close(ext.release)
		}
		s.Close()
	})

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal("Cannot connect to server:", err)
	}
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("Did not receive greeting:", scanner.Err())
	}
	if !strings.HasPrefix(scanner.Text(), "* OK ") {
		t.Fatalf("Unexpected greeting: %q", scanner.Text())
	}

	if _, err := conn.Write([]byte("a001 SELECT INBOX\r\n")); err != nil {
		t.Fatal("Cannot send SELECT:", err)
	}
	<-ext.started

	closed := make(chan error, 1)
	go func() { closed <- s.Shutdown() }()
	readErr := make(chan error, 1)
	go func() {
		_, err := conn.Read(make([]byte, 1))
		readErr <- err
	}()
	select {
	case err := <-readErr:
		if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Unexpected error after server shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Server.Close did not close the connection transport")
	}

	select {
	case err := <-closed:
		t.Fatalf("Server.Close returned before command handler exited: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-ext.mailbox.(*closeTrackingMailbox).closed:
		t.Fatal("Server.Close closed mailbox while command handler was running")
	default:
	}

	close(ext.release)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal("Cannot close server:", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Server.Close did not return after command handler exited")
	}
	select {
	case <-ext.mailbox.(*closeTrackingMailbox).closed:
	case <-time.After(time.Second):
		t.Fatal("Connection cleanup did not close mailbox after command returned")
	}
}
