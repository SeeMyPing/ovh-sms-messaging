package smpp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/linxGnu/gosmpp/data"
	"github.com/linxGnu/gosmpp/pdu"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
)

func newTestClient(t *testing.T, smsc *fakeSMSC) *Client {
	t.Helper()
	c := NewClient(Config{
		Addr:           smsc.addr(),
		SystemID:       "user",
		Password:       "secret",
		SourceAddr:     "DEFAULT",
		ConnectTimeout: time.Second,
		SubmitTimeout:  time.Second,
		EnquireLink:    time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.rebindInterval = 50 * time.Millisecond
	t.Cleanup(func() { c.Close() })
	return c
}

func TestSendShort(t *testing.T) {
	smsc := newFakeSMSC(t)
	c := newTestClient(t, smsc)

	ids, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hello"})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "msg-1" {
		t.Fatalf("ids = %v, want [msg-1]", ids)
	}

	subs := smsc.submitted()
	if len(subs) != 1 {
		t.Fatalf("got %d submit_sm, want 1", len(subs))
	}
	p := subs[0]
	if got := p.DestAddr; got.Ton() != data.GSM_TON_INTERNATIONAL || got.Npi() != data.GSM_NPI_E164 || got.Address() != "33612345678" {
		t.Errorf("dest = ton %d npi %d %q", got.Ton(), got.Npi(), got.Address())
	}
	if got := p.SourceAddr; got.Ton() != data.GSM_TON_ALPHANUMERIC || got.Address() != "DEFAULT" {
		t.Errorf("source = ton %d %q, want alphanumeric DEFAULT", got.Ton(), got.Address())
	}
	if p.EsmClass&data.SM_UDH_GSM != 0 {
		t.Errorf("esm_class = %#x, want no UDH", p.EsmClass)
	}
	if msg, _ := p.Message.GetMessageWithEncoding(data.GSM7BIT); msg != "hello" {
		t.Errorf("message = %q", msg)
	}
	if smsc.bindCount() != 1 {
		t.Errorf("binds = %d, want 1", smsc.bindCount())
	}
}

func TestSendSourceAddress(t *testing.T) {
	tests := []struct {
		sender   string
		wantTon  byte
		wantAddr string
	}{
		{sender: "MYAPP", wantTon: data.GSM_TON_ALPHANUMERIC, wantAddr: "MYAPP"},
		{sender: "+33700000000", wantTon: data.GSM_TON_INTERNATIONAL, wantAddr: "33700000000"},
		{sender: "36180", wantTon: data.GSM_TON_UNKNOWN, wantAddr: "36180"},
	}
	smsc := newFakeSMSC(t)
	c := newTestClient(t, smsc)
	for i, tt := range tests {
		if _, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi", Sender: tt.sender}); err != nil {
			t.Fatalf("Send(%q) error: %v", tt.sender, err)
		}
		src := smsc.submitted()[i].SourceAddr
		if src.Ton() != tt.wantTon || src.Address() != tt.wantAddr {
			t.Errorf("sender %q: source = ton %d %q, want ton %d %q", tt.sender, src.Ton(), src.Address(), tt.wantTon, tt.wantAddr)
		}
	}
}

func TestSendLong(t *testing.T) {
	smsc := newFakeSMSC(t)
	c := newTestClient(t, smsc)

	text := strings.Repeat("ç", 100) // not in GSM 03.38: UCS-2, 200 bytes, 2 parts
	ids, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: text})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v, want 2", ids)
	}

	subs := smsc.submitted()
	var ref byte
	var got string
	for i, p := range subs {
		if p.EsmClass&data.SM_UDH_GSM == 0 {
			t.Errorf("part %d: esm_class = %#x, want UDH flag", i+1, p.EsmClass)
		}
		if p.Message.Encoding().DataCoding() != data.UCS2Coding {
			t.Errorf("part %d: data coding = %d, want UCS-2", i+1, p.Message.Encoding().DataCoding())
		}
		total, num, r, ok := p.Message.UDH().GetConcatInfo()
		if !ok || total != 2 || int(num) != i+1 {
			t.Errorf("part %d: concat = %d/%d (found %v)", i+1, num, total, ok)
		}
		if i == 0 {
			ref = r
		} else if r != ref {
			t.Errorf("part %d: reference %d, want %d", i+1, r, ref)
		}
		part, _ := p.Message.GetMessageWithEncoding(data.UCS2)
		got += part
	}
	if subs[0].SequenceNumber == subs[1].SequenceNumber {
		t.Errorf("parts share sequence number %d", subs[0].SequenceNumber)
	}
	if got != text {
		t.Errorf("reassembled message differs: %q", got)
	}
}

func TestSendStatusErrors(t *testing.T) {
	tests := []struct {
		status        data.CommandStatusType
		wantPermanent bool
	}{
		{data.ESME_RINVDSTADR, true},
		{data.ESME_RINVMSGLEN, true},
		{data.ESME_RTHROTTLED, false},
		{data.ESME_RMSGQFUL, false},
		{data.ESME_RSYSERR, false},
		{data.ESME_RINVSRCADR, false},
	}
	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			smsc := newFakeSMSC(t)
			smsc.status = func(*pdu.SubmitSM) data.CommandStatusType { return tt.status }
			c := newTestClient(t, smsc)

			_, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi"})
			var statusErr *StatusError
			if !errors.As(err, &statusErr) || statusErr.Status != tt.status {
				t.Fatalf("error = %v, want StatusError %s", err, tt.status)
			}
			if statusErr.Permanent() != tt.wantPermanent {
				t.Errorf("Permanent() = %v, want %v", statusErr.Permanent(), tt.wantPermanent)
			}
		})
	}
}

func TestSendBindFailure(t *testing.T) {
	smsc := newFakeSMSC(t)
	smsc.password = "other"
	c := newTestClient(t, smsc)

	_, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi"})
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != data.ESME_RINVPASWD {
		t.Fatalf("error = %v, want StatusError ESME_RINVPASWD", err)
	}
	if statusErr.Permanent() {
		t.Error("bind failure must not be permanent")
	}

	// The next send binds again, and succeeds once the password is fixed.
	smsc.mu.Lock()
	smsc.password = "secret"
	smsc.mu.Unlock()
	if _, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi"}); err != nil {
		t.Fatalf("Send after fixing the password: %v", err)
	}
}

func TestSendUnreachable(t *testing.T) {
	smsc := newFakeSMSC(t)
	smsc.close()
	c := newTestClient(t, smsc)

	if _, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi"}); err == nil {
		t.Fatal("Send succeeded with the SMSC down")
	}
}

func TestSendTimeout(t *testing.T) {
	smsc := newFakeSMSC(t)
	smsc.silent = true
	c := newTestClient(t, smsc)
	c.cfg.SubmitTimeout = 100 * time.Millisecond

	start := time.Now()
	_, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("Send took %v", d)
	}
}

func TestSendReconnects(t *testing.T) {
	smsc := newFakeSMSC(t)
	smsc.dropAfter = 1
	c := newTestClient(t, smsc)

	if _, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi"}); err == nil {
		t.Fatal("Send succeeded although the SMSC dropped the connection")
	}

	// The session rebinds in the background; retry until it is back.
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi"})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Send still failing after rebind: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if smsc.bindCount() < 2 {
		t.Errorf("binds = %d, want a rebind", smsc.bindCount())
	}
}

func TestSendConcurrent(t *testing.T) {
	smsc := newFakeSMSC(t)
	c := newTestClient(t, smsc)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: fmt.Sprintf("msg %d", i)})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Send error: %v", err)
		}
	}
	if got := len(smsc.submitted()); got != n {
		t.Errorf("submitted %d, want %d", got, n)
	}
	if smsc.bindCount() != 1 {
		t.Errorf("binds = %d, want 1 shared session", smsc.bindCount())
	}
}
