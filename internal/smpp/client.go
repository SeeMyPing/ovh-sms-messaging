// Package smpp sends SMS to an SMSC over SMPP 3.4.
//
// The client binds as a transmitter on the first send and keeps the session
// open, sending enquire_link to keep it alive and rebinding if it drops.
// Each submit_sm waits for its submit_sm_resp, so Send is synchronous.
package smpp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/linxGnu/gosmpp"
	"github.com/linxGnu/gosmpp/data"
	"github.com/linxGnu/gosmpp/pdu"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
)

// defaultRebindInterval is the delay between two bind attempts once the
// session has dropped.
const defaultRebindInterval = 5 * time.Second

// errConnectionLost is returned for requests pending when the session drops.
var errConnectionLost = errors.New("smpp connection lost")

// Config holds the SMSC connection settings.
type Config struct {
	// Addr is the SMSC host:port.
	Addr       string
	TLS        bool
	SystemID   string
	Password   string
	SystemType string
	// SourceAddr is the sender used when the message does not set its own.
	SourceAddr string
	// ConnectTimeout bounds the TCP connection and the bind.
	ConnectTimeout time.Duration
	// SubmitTimeout bounds the wait for each submit_sm_resp.
	SubmitTimeout time.Duration
	// EnquireLink is the keep-alive period.
	EnquireLink time.Duration
}

// StatusError is a non-zero command_status returned by the SMSC.
type StatusError struct {
	Status data.CommandStatusType
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("smpp status %s (0x%08X): %s", e.Status, int32(e.Status), e.Status.Desc())
}

// Permanent reports whether the message itself is at fault, so that
// resending it cannot succeed. Other statuses (throttling, SMSC failure,
// rejected sender or credentials...) may clear up or depend on the
// configuration: those messages are retried, then kept in the dead letter
// queue.
func (e *StatusError) Permanent() bool {
	switch e.Status {
	case data.ESME_RINVDSTADR, data.ESME_RINVDSTTON, data.ESME_RINVDSTNPI, data.ESME_RINVMSGLEN:
		return true
	}
	return false
}

// Client submits SMS over a shared SMPP session.
type Client struct {
	cfg            Config
	logger         *slog.Logger
	rebindInterval time.Duration

	mu      sync.Mutex // guards session
	session *gosmpp.Session

	pendingMu sync.Mutex
	pending   map[int32]chan result

	concatRef atomic.Uint32
}

type result struct {
	pdu pdu.PDU
	err error
}

// NewClient returns a Client; it connects on the first Send.
func NewClient(cfg Config, logger *slog.Logger) *Client {
	return &Client{
		cfg:            cfg,
		logger:         logger,
		rebindInterval: defaultRebindInterval,
		pending:        make(map[int32]chan result),
	}
}

// Send submits sms and returns the message IDs assigned by the SMSC, one per
// SMS part. A long message is sent as concatenated parts: if a part fails,
// the error is returned and a retry resends every part.
func (c *Client) Send(ctx context.Context, sms message.SMS) ([]string, error) {
	sender := sms.Sender
	if sender == "" {
		sender = c.cfg.SourceAddr
	}
	src, err := sourceAddress(sender)
	if err != nil {
		return nil, err
	}
	dst, err := pdu.NewAddressWithTonNpiAddr(data.GSM_TON_INTERNATIONAL, data.GSM_NPI_E164, strings.TrimPrefix(sms.To, "+"))
	if err != nil {
		return nil, fmt.Errorf("destination address: %w", err)
	}
	enc, segments, err := encode(sms.Message)
	if err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}

	session, err := c.connect()
	if err != nil {
		return nil, err
	}

	ref := byte(c.concatRef.Add(1))
	ids := make([]string, 0, len(segments))
	for i, segment := range segments {
		// NewSubmitSM assigns a fresh sequence number to each part.
		p := pdu.NewSubmitSM().(*pdu.SubmitSM)
		p.SourceAddr = src
		p.DestAddr = dst
		if err := p.Message.SetMessageDataWithEncoding(segment, enc); err != nil {
			return ids, fmt.Errorf("part %d/%d: %w", i+1, len(segments), err)
		}
		if len(segments) > 1 {
			p.EsmClass |= data.SM_UDH_GSM
			p.Message.SetUDH(pdu.UDH{pdu.NewIEConcatMessage(byte(len(segments)), byte(i+1), ref)})
		}

		id, err := c.submit(ctx, session, p)
		if err != nil {
			if len(segments) > 1 {
				err = fmt.Errorf("part %d/%d: %w", i+1, len(segments), err)
			}
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Close unbinds and closes the session, if any.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return nil
	}
	err := c.session.Close()
	c.session = nil
	return err
}

func (c *Client) submit(ctx context.Context, session *gosmpp.Session, p *pdu.SubmitSM) (string, error) {
	seq := p.GetSequenceNumber()
	ch := make(chan result, 1)
	c.pendingMu.Lock()
	c.pending[seq] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, seq)
		c.pendingMu.Unlock()
	}()

	if err := session.Transmitter().Submit(p); err != nil {
		return "", fmt.Errorf("submit_sm: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, c.cfg.SubmitTimeout)
	defer cancel()
	var r result
	select {
	case r = <-ch:
	case <-ctx.Done():
		return "", fmt.Errorf("waiting for submit_sm_resp: %w", ctx.Err())
	}
	if r.err != nil {
		return "", r.err
	}

	if status := r.pdu.GetHeader().CommandStatus; status != data.ESME_ROK {
		return "", &StatusError{Status: status}
	}
	resp, ok := r.pdu.(*pdu.SubmitSMResp)
	if !ok {
		return "", fmt.Errorf("unexpected response %T to submit_sm", r.pdu)
	}
	return resp.MessageID, nil
}

// connect returns the session, binding on first use. Once created, the
// session rebinds by itself when the connection drops.
func (c *Client) connect() (*gosmpp.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		return c.session, nil
	}

	auth := gosmpp.Auth{
		SMSC:       c.cfg.Addr,
		SystemID:   c.cfg.SystemID,
		Password:   c.cfg.Password,
		SystemType: c.cfg.SystemType,
	}
	settings := gosmpp.Settings{
		EnquireLink: c.cfg.EnquireLink,
		// Without traffic, the SMSC answers enquire_link at least every
		// EnquireLink: a longer silence means the connection is dead.
		ReadTimeout:  2 * c.cfg.EnquireLink,
		WriteTimeout: c.cfg.SubmitTimeout,
		OnPDU:        c.onPDU,
		OnSubmitError: func(p pdu.PDU, err error) {
			c.resolve(p.GetSequenceNumber(), result{err: fmt.Errorf("write %T: %w", p, err)})
		},
		OnReceivingError: func(err error) {
			c.logger.Warn("smpp receive error", "error", err)
		},
		OnRebindingError: func(err error) {
			c.logger.Warn("smpp rebind failed", "error", err)
		},
		OnClosed: func(state gosmpp.State) {
			c.logger.Warn("smpp session closed", "state", state.String())
			c.failPending()
		},
		OnRebind: func() {
			c.logger.Info("smpp session rebound")
		},
	}

	session, err := gosmpp.NewSession(gosmpp.TXConnector(c.dial, auth), settings, c.rebindInterval)
	if err != nil {
		var bindErr gosmpp.BindError
		if errors.As(err, &bindErr) {
			return nil, fmt.Errorf("smpp bind: %w", &StatusError{Status: bindErr.CommandStatus})
		}
		return nil, fmt.Errorf("smpp connect: %w", err)
	}
	c.logger.Info("smpp session bound", "addr", c.cfg.Addr, "system_id", c.cfg.SystemID)
	c.session = session
	return session, nil
}

// dial opens the TCP (or TLS) connection. The deadline also bounds the bind;
// the session replaces it with its own read and write timeouts afterwards.
func (c *Client) dial(addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: c.cfg.ConnectTimeout}
	var conn net.Conn
	var err error
	if c.cfg.TLS {
		conn, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.Dial("tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(c.cfg.ConnectTimeout)); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (c *Client) onPDU(p pdu.PDU, _ bool) {
	switch p.(type) {
	case *pdu.SubmitSMResp, *pdu.GenericNack:
		c.resolve(p.GetSequenceNumber(), result{pdu: p})
	case *pdu.DeliverSM:
		// Transmitter binds should not receive any; delivery receipts are
		// not handled.
		c.logger.Debug("smpp deliver_sm ignored")
	}
}

func (c *Client) resolve(seq int32, r result) {
	c.pendingMu.Lock()
	ch, ok := c.pending[seq]
	c.pendingMu.Unlock()
	if ok {
		select {
		case ch <- r:
		default:
		}
	}
}

func (c *Client) failPending() {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for _, ch := range c.pending {
		select {
		case ch <- result{err: errConnectionLost}:
		default:
		}
	}
}

// sourceAddress sets the TON/NPI matching the sender format validated by
// message.NormalizeSender.
func sourceAddress(sender string) (pdu.Address, error) {
	var addr pdu.Address
	var err error
	switch {
	case strings.HasPrefix(sender, "+"):
		addr, err = pdu.NewAddressWithTonNpiAddr(data.GSM_TON_INTERNATIONAL, data.GSM_NPI_E164, sender[1:])
	case isDigits(sender):
		addr, err = pdu.NewAddressWithTonNpiAddr(data.GSM_TON_UNKNOWN, data.GSM_NPI_E164, sender)
	default:
		addr, err = pdu.NewAddressWithTonNpiAddr(data.GSM_TON_ALPHANUMERIC, data.GSM_NPI_UNKNOWN, sender)
	}
	if err != nil {
		return pdu.Address{}, fmt.Errorf("source address: %w", err)
	}
	return addr, nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
