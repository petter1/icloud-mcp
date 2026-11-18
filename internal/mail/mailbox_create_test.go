package mail

import (
	"context"
	"strings"
	"testing"

	"github.com/ThomasCrouzet/icloud-mcp/internal/mail/imapadapter"
)

func createTestMailboxes() []imapadapter.Mailbox {
	return []imapadapter.Mailbox{
		{Name: "INBOX", Delimiter: '/'},
		{Name: "Archive", Delimiter: '/', Attributes: []string{"\\Archive"}},
		{Name: "Projects", Delimiter: '/', Attributes: []string{"\\HasNoChildren"}},
		{Name: "Shared", Delimiter: '/', Attributes: []string{"\\Noselect"}},
	}
}

func TestCreateMailboxCreatesAndVerifies(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"deleted by claude", "Projects/2027"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			imap := &fakeIMAP{mailboxes: createTestMailboxes()}
			result, err := testService(t, imap, nil, true, false).CreateMailbox(context.Background(), CreateMailboxInput{Name: name})
			if err != nil {
				t.Fatalf("create mailbox: %v", err)
			}
			if !result.Created || result.Name != name || result.Delimiter != "/" || result.ResultIncomplete {
				t.Fatalf("unexpected result: %#v", result)
			}
			if got := strings.Join(imap.log, ","); got != "list,create,list,close" {
				t.Fatalf("unexpected IMAP sequence: %s", got)
			}
		})
	}
}

func TestCreateMailboxExistingSelectableIsNoop(t *testing.T) {
	t.Parallel()
	imap := &fakeIMAP{mailboxes: createTestMailboxes()}
	result, err := testService(t, imap, nil, true, false).CreateMailbox(context.Background(), CreateMailboxInput{Name: "Projects"})
	if err != nil {
		t.Fatalf("existing mailbox: %v", err)
	}
	if result.Created || result.Name != "Projects" || len(result.Attributes) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if strings.Contains(strings.Join(imap.log, ","), "create") {
		t.Fatalf("existing mailbox sent CREATE: %v", imap.log)
	}
}

func TestCreateMailboxRejectsBeforeDispatch(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		name string
		code Code
	}{
		"empty":          {"", CodeValidation},
		"inbox":          {"inbox", CodeValidation},
		"wildcard star":  {"Old*", CodeValidation},
		"wildcard pct":   {"100%", CodeValidation},
		"padded":         {" Archive2", CodeValidation},
		"control":        {"bad\nname", CodeValidation},
		"empty level":    {"Projects//2027", CodeValidation},
		"trailing delim": {"Projects/", CodeValidation},
		"missing parent": {"Clients/2027", CodeNotFound},
		"case collision": {"archive", CodeConflict},
		"noselect":       {"Shared", CodeConflict},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			imap := &fakeIMAP{mailboxes: createTestMailboxes()}
			_, err := testService(t, imap, nil, true, false).CreateMailbox(context.Background(), CreateMailboxInput{Name: tc.name})
			if got := errorCode(t, err); got != tc.code {
				t.Fatalf("code = %s, want %s (err %v)", got, tc.code, err)
			}
			if strings.Contains(strings.Join(imap.log, ","), "create") {
				t.Fatalf("rejected name reached CREATE: %v", imap.log)
			}
		})
	}
}

func TestCreateMailboxRequiresMutation(t *testing.T) {
	t.Parallel()
	imap := &fakeIMAP{mailboxes: createTestMailboxes()}
	_, err := testService(t, imap, nil, false, false).CreateMailbox(context.Background(), CreateMailboxInput{Name: "New"})
	if errorCode(t, err) != CodeAuthorization {
		t.Fatalf("disabled mutation error: %v", err)
	}
	if len(imap.log) != 0 {
		t.Fatalf("disabled mutation touched IMAP: %v", imap.log)
	}
}

func TestCreateMailboxServerErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		err  error
		code Code
	}{
		"already exists": {&imapadapter.Error{Kind: imapadapter.ErrorConflict}, CodeConflict},
		"ambiguous":      {&imapadapter.Error{Kind: imapadapter.ErrorUnavailable, Ambiguous: true}, CodeOutcomeUnknown},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			imap := &fakeIMAP{mailboxes: createTestMailboxes(), errors: map[string]error{"create": tc.err}}
			_, err := testService(t, imap, nil, true, false).CreateMailbox(context.Background(), CreateMailboxInput{Name: "New"})
			if got := errorCode(t, err); got != tc.code {
				t.Fatalf("code = %s, want %s (err %v)", got, tc.code, err)
			}
		})
	}
}

func TestCreateMailboxIncompleteWhenNotListedAfterCreate(t *testing.T) {
	t.Parallel()
	calls := 0
	imap := &fakeIMAP{}
	imap.listFn = func() ([]imapadapter.Mailbox, error) {
		calls++
		return createTestMailboxes(), nil
	}
	result, err := testService(t, imap, nil, true, false).CreateMailbox(context.Background(), CreateMailboxInput{Name: "New"})
	if err != nil {
		t.Fatalf("create mailbox: %v", err)
	}
	if !result.Created || !result.ResultIncomplete || calls != 2 {
		t.Fatalf("unexpected result %#v after %d LIST calls", result, calls)
	}
}
