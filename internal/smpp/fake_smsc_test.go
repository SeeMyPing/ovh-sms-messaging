package smpp

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/linxGnu/gosmpp/data"
	"github.com/linxGnu/gosmpp/pdu"
)

// fakeSMSC is a minimal SMSC: it accepts transmitter binds, answers
// submit_sm, enquire_link and unbind, and records the submitted PDUs.
type fakeSMSC struct {
	t        *testing.T
	ln       net.Listener
	password string

	mu sync.Mutex
	// status returns the command_status of a submit_sm_resp. nil means OK.
	status func(*pdu.SubmitSM) data.CommandStatusType
	// silent drops submit_sm without answering.
	silent bool
	// dropAfter closes the connection after that many submit_sm (0: never).
	dropAfter int
	submits   []*pdu.SubmitSM
	binds     int
	conns     []net.Conn
}

func newFakeSMSC(t *testing.T) *fakeSMSC {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMSC{t: t, ln: ln, password: "secret"}
	go f.accept()
	t.Cleanup(f.close)
	return f
}

func (f *fakeSMSC) addr() string { return f.ln.Addr().String() }

func (f *fakeSMSC) close() {
	f.ln.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		c.Close()
	}
}

func (f *fakeSMSC) accept() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns = append(f.conns, conn)
		f.mu.Unlock()
		go f.serve(conn)
	}
}

func (f *fakeSMSC) serve(conn net.Conn) {
	defer conn.Close()
	for {
		p, err := pdu.Parse(conn)
		if err != nil {
			return
		}
		switch p := p.(type) {
		case *pdu.BindRequest:
			resp := p.GetResponse().(*pdu.BindResp)
			resp.SystemID = "fake"
			f.mu.Lock()
			f.binds++
			password := f.password
			f.mu.Unlock()
			if p.Password != password {
				// SMPP 3.4: a failed bind_resp has no body, only the header.
				hdr := make([]byte, 16)
				binary.BigEndian.PutUint32(hdr[0:], 16)
				binary.BigEndian.PutUint32(hdr[4:], uint32(resp.CommandID))
				binary.BigEndian.PutUint32(hdr[8:], uint32(data.ESME_RINVPASWD))
				binary.BigEndian.PutUint32(hdr[12:], uint32(p.SequenceNumber))
				_, _ = conn.Write(hdr)
				return
			}
			write(conn, resp)

		case *pdu.SubmitSM:
			f.mu.Lock()
			f.submits = append(f.submits, p)
			n := len(f.submits)
			silent, drop, status := f.silent, f.dropAfter > 0 && n >= f.dropAfter, f.status
			if drop {
				f.dropAfter = 0
			}
			f.mu.Unlock()

			if drop {
				return
			}
			if silent {
				continue
			}
			resp := p.GetResponse().(*pdu.SubmitSMResp)
			resp.MessageID = fmt.Sprintf("msg-%d", n)
			if status != nil {
				resp.CommandStatus = status(p)
			}
			write(conn, resp)

		case *pdu.EnquireLink:
			write(conn, p.GetResponse())

		case *pdu.Unbind:
			write(conn, p.GetResponse())
			return
		}
	}
}

func (f *fakeSMSC) submitted() []*pdu.SubmitSM {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*pdu.SubmitSM(nil), f.submits...)
}

func (f *fakeSMSC) bindCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.binds
}

func write(conn net.Conn, p pdu.PDU) {
	buf := pdu.NewBuffer(make([]byte, 0, 64))
	p.Marshal(buf)
	_, _ = conn.Write(buf.Bytes())
}
