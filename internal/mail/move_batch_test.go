package mail

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ThomasCrouzet/icloud-mcp/internal/mail/imapadapter"
)

func batchTestIMAP(caps imapadapter.Capabilities) *fakeIMAP {
	return &fakeIMAP{
		caps:      caps,
		selected:  imapadapter.SelectedMailbox{UIDValidity: 4},
		mailboxes: []imapadapter.Mailbox{{Name: "INBOX"}, {Name: "Archive"}, {Name: "Trash", Attributes: []string{"\\Trash"}}},
	}
}

func TestMoveMessagesNativeReturnsNewUIDsInOrder(t *testing.T) {
	t.Parallel()
	imap := batchTestIMAP(imapadapter.Capabilities{Move: true})
	next := uint32(100)
	imap.nativeMoveFn = func() (imapadapter.CopyData, error) {
		next++
		return imapadapter.CopyData{UIDValidity: 9, DestinationUID: next}, nil
	}
	input := MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{7, 3, 5}, Destination: "Archive"}
	result, err := testService(t, imap, nil, true, false).MoveMessages(context.Background(), input)
	if err != nil {
		t.Fatalf("batch move: %v", err)
	}
	want := []MovedMessage{
		{UID: 7, DestinationUIDValidity: 9, DestinationUID: 101, Method: "move"},
		{UID: 3, DestinationUIDValidity: 9, DestinationUID: 102, Method: "move"},
		{UID: 5, DestinationUIDValidity: 9, DestinationUID: 103, Method: "move"},
	}
	if !result.Complete || result.Failed != nil || len(result.NotAttempted) != 0 || !reflect.DeepEqual(result.Moved, want) {
		t.Fatalf("unexpected result: %#v", result)
	}
	got := strings.Join(imap.log, ",")
	wantLog := "list,select,fetch-flags,fetch-flags,fetch-flags,native-move,native-move,native-move,close"
	if got != wantLog {
		t.Fatalf("IMAP sequence = %s, want %s", got, wantLog)
	}
}

func TestMoveMessagesUIDPlusFallback(t *testing.T) {
	t.Parallel()
	imap := batchTestIMAP(imapadapter.Capabilities{UIDPlus: true})
	imap.copyData = imapadapter.CopyData{UIDValidity: 9, DestinationUID: 50}
	input := MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{1, 2}, Destination: "Archive"}
	result, err := testService(t, imap, nil, true, false).MoveMessages(context.Background(), input)
	if err != nil {
		t.Fatalf("fallback batch move: %v", err)
	}
	if !result.Complete || len(result.Moved) != 2 || result.Moved[1].Method != "uidplus_fallback" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if strings.Count(strings.Join(imap.log, ","), "uid-expunge") != 2 {
		t.Fatalf("fallback must expunge each UID by UID: %v", imap.log)
	}
}

func TestMoveMessagesStopsAfterPartialFailure(t *testing.T) {
	t.Parallel()
	imap := batchTestIMAP(imapadapter.Capabilities{Move: true})
	calls := 0
	imap.nativeMoveFn = func() (imapadapter.CopyData, error) {
		calls++
		if calls == 2 {
			return imapadapter.CopyData{}, &imapadapter.Error{Kind: imapadapter.ErrorUnavailable, Ambiguous: true}
		}
		return imapadapter.CopyData{UIDValidity: 9, DestinationUID: uint32(200 + calls)}, nil
	}
	input := MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{1, 2, 3, 4}, Destination: "Archive"}
	result, err := testService(t, imap, nil, true, false).MoveMessages(context.Background(), input)
	if err != nil {
		t.Fatalf("partial batch must return a result: %v", err)
	}
	if result.Complete || len(result.Moved) != 1 || result.Moved[0].UID != 1 {
		t.Fatalf("unexpected moved set: %#v", result)
	}
	if result.Failed == nil || result.Failed.UID != 2 || result.Failed.Code != CodeOutcomeUnknown || result.Failed.Reconciliation == "" {
		t.Fatalf("unexpected failure: %#v", result.Failed)
	}
	if !reflect.DeepEqual(result.NotAttempted, []uint32{3, 4}) || calls != 2 {
		t.Fatalf("batch continued after failure: notAttempted=%v calls=%d", result.NotAttempted, calls)
	}
}

func TestMoveMessagesFirstFailureIsAnError(t *testing.T) {
	t.Parallel()
	imap := batchTestIMAP(imapadapter.Capabilities{Move: true})
	imap.errors = map[string]error{"move": &imapadapter.Error{Kind: imapadapter.ErrorAuthorization}}
	input := MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{1, 2}, Destination: "Archive"}
	_, err := testService(t, imap, nil, true, false).MoveMessages(context.Background(), input)
	if err == nil || errorCode(t, err) == "" {
		t.Fatalf("first failure must be returned as an error: %v", err)
	}
	if strings.Count(strings.Join(imap.log, ","), "native-move") != 1 {
		t.Fatalf("batch continued after the first failure: %v", imap.log)
	}
}

func TestMoveMessagesRejectsBeforeDispatch(t *testing.T) {
	t.Parallel()
	tooMany := make([]uint32, MaxMoveBatch+1)
	for i := range tooMany {
		tooMany[i] = uint32(i + 1)
	}
	cases := map[string]struct {
		input  MoveBatchInput
		code   Code
		mutate func(*fakeIMAP)
	}{
		"empty uids":     {MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, Destination: "Archive"}, CodeValidation, nil},
		"too many":       {MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: tooMany, Destination: "Archive"}, CodeValidation, nil},
		"duplicate":      {MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{1, 1}, Destination: "Archive"}, CodeValidation, nil},
		"zero uid":       {MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{0}, Destination: "Archive"}, CodeValidation, nil},
		"same mailbox":   {MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{1}, Destination: "INBOX"}, CodeValidation, nil},
		"trash target":   {MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{1}, Destination: "Trash"}, CodeValidation, nil},
		"unknown target": {MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{1}, Destination: "Nowhere"}, CodeNotFound, nil},
		"stale validity": {MoveBatchInput{Mailbox: "INBOX", UIDValidity: 5, UIDs: []uint32{1}, Destination: "Archive"}, CodeConcurrentModification, nil},
		"missing message": {MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{1, 2}, Destination: "Archive"}, CodeNotFound, func(f *fakeIMAP) {
			f.errors = map[string]error{"flags": &imapadapter.Error{Kind: imapadapter.ErrorNotFound}}
		}},
		"no move or plus": {MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{1}, Destination: "Archive"}, CodeProtocolError, func(f *fakeIMAP) { f.caps = imapadapter.Capabilities{} }},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			imap := batchTestIMAP(imapadapter.Capabilities{Move: true})
			if tc.mutate != nil {
				tc.mutate(imap)
			}
			_, err := testService(t, imap, nil, true, false).MoveMessages(context.Background(), tc.input)
			if got := errorCode(t, err); got != tc.code {
				t.Fatalf("code = %s, want %s (err %v)", got, tc.code, err)
			}
			joined := strings.Join(imap.log, ",")
			if strings.Contains(joined, "move") || strings.Contains(joined, "copy") {
				t.Fatalf("rejected batch reached the mutation boundary: %v", imap.log)
			}
		})
	}
}

func TestMoveMessagesRequiresMutation(t *testing.T) {
	t.Parallel()
	imap := batchTestIMAP(imapadapter.Capabilities{Move: true})
	input := MoveBatchInput{Mailbox: "INBOX", UIDValidity: 4, UIDs: []uint32{1}, Destination: "Archive"}
	_, err := testService(t, imap, nil, false, false).MoveMessages(context.Background(), input)
	if errorCode(t, err) != CodeAuthorization || len(imap.log) != 0 {
		t.Fatalf("disabled mutation: err=%v log=%v", err, imap.log)
	}
}
