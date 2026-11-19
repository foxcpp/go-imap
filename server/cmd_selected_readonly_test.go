package server

import (
	"testing"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
)

type selectedTestConn struct {
	Conn
	ctx *Context
}

func (c *selectedTestConn) Context() *Context {
	return c.ctx
}

type selectedTestMailbox struct {
	backend.Mailbox
	expungeCalls int
	moveCalls    int
}

func (m *selectedTestMailbox) Expunge() error {
	m.expungeCalls++
	return nil
}

func (m *selectedTestMailbox) MoveMessages(bool, *imap.SeqSet, string) error {
	m.moveCalls++
	return nil
}

func TestClose_ReadOnlyDoesNotExpunge(t *testing.T) {
	mailbox := &selectedTestMailbox{}
	ctx := &Context{Mailbox: mailbox, MailboxReadOnly: true}
	conn := &selectedTestConn{ctx: ctx}

	if err := (&Close{}).Handle(conn); err != nil {
		t.Fatalf("Close returned an error: %v", err)
	}
	if mailbox.expungeCalls != 0 {
		t.Fatalf("Close expunged a read-only mailbox %d times", mailbox.expungeCalls)
	}
	if ctx.Mailbox != nil || ctx.MailboxReadOnly {
		t.Fatalf("Close did not clear selected mailbox state: %+v", ctx)
	}
}

func TestMove_ReadOnlyRejected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		handle func(*Move, Conn) error
	}{
		{name: "MOVE", handle: (*Move).Handle},
		{name: "UID MOVE", handle: (*Move).UidHandle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mailbox := &selectedTestMailbox{}
			conn := &selectedTestConn{ctx: &Context{Mailbox: mailbox, MailboxReadOnly: true}}

			if err := tc.handle(&Move{}, conn); err != ErrMailboxReadOnly {
				t.Fatalf("Move returned %v, want %v", err, ErrMailboxReadOnly)
			}
			if mailbox.moveCalls != 0 {
				t.Fatalf("MoveMessages was called %d times", mailbox.moveCalls)
			}
		})
	}
}
