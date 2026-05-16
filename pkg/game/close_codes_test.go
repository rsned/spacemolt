package game

import (
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rsned/spacemolt/internal/protocol"
)

func TestClosePolicy_Known1000_DocumentedReason(t *testing.T) {
	p, known := lookupClosePolicy(websocket.StatusNormalClosure, "max session age")
	if !known {
		t.Fatal("expected known=true for documented close=1000")
	}
	if p.action != closeReplay {
		t.Errorf("action = %v, want closeReplay", p.action)
	}
}

func TestClosePolicy_Known1000_UndocumentedReason(t *testing.T) {
	p, known := lookupClosePolicy(websocket.StatusNormalClosure, "region migration")
	if known {
		t.Fatal("expected known=false for undocumented reason on 1000")
	}
	// Even unknown reason on 1000 still defaults to replay (per spec).
	if p.action != closeReplay {
		t.Errorf("action = %v, want closeReplay fallback", p.action)
	}
}

func TestClosePolicy_KnownFailFast(t *testing.T) {
	cases := []websocket.StatusCode{
		websocket.StatusGoingAway,
		websocket.StatusAbnormalClosure,
		websocket.StatusInternalError,
	}
	for _, code := range cases {
		p, known := lookupClosePolicy(code, "")
		if !known {
			t.Errorf("code=%d should be known", code)
		}
		if p.action != closeFailFast {
			t.Errorf("code=%d action = %v, want closeFailFast", code, p.action)
		}
	}
}

func TestClosePolicy_Unknown(t *testing.T) {
	p, known := lookupClosePolicy(websocket.StatusCode(4999), "wat")
	if known {
		t.Fatal("expected known=false for code=4999")
	}
	if p.action != closeFailFast {
		t.Errorf("unknown code action = %v, want closeFailFast", p.action)
	}
}

func TestHandleClose_FailFastPropagates(t *testing.T) {
	c := newSubmitClientSkeleton()
	// Register a fake pending subscription.
	ch := make(chan protocol.Response, 1)
	c.router.registerByID("req-fast", ch, terminateOnAction)

	closeErr := &websocket.CloseError{Code: websocket.StatusGoingAway, Reason: "test"}
	c.handleClose(closeErr)

	select {
	case resp := <-ch:
		if resp.Type != protocol.TypeError {
			t.Errorf("resp.Type = %q, want error", resp.Type)
		}
		if code, _ := resp.Payload["code"].(string); code != "connection_closed" {
			t.Errorf("code = %q, want connection_closed", code)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("pending request not failed on close")
	}
}
