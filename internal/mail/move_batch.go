package mail

import "context"

// MoveMessages moves several messages from one source mailbox to one
// destination in a single IMAP session. It applies the move_message checks
// once, confirms that every UID exists before the first move, and then moves
// the UIDs one by one in request order with native UID MOVE or the UIDPLUS
// fallback. It stops at the first failure. When nothing was moved yet, the
// failure is returned as an error. Otherwise the result lists the moved UIDs,
// the failed UID, and the UIDs that were not attempted.
func (s *Client) MoveMessages(ctx context.Context, input MoveBatchInput) (MoveBatchResult, error) {
	if err := validateMoveBatch(input); err != nil {
		return MoveBatchResult{}, err
	}
	const reconciliation = "Check both source and destination before deciding whether to move again."
	value, err := s.mutate(ctx, reconciliation, func(session imapSession, phase *imapMutationPhase) (any, error) {
		mailboxes, err := boundedMailboxList(session)
		if err != nil {
			return nil, err
		}
		if specialUseDeletionMailbox(mailboxes, input.Destination) {
			return nil, validationError("destination cannot be a SPECIAL-USE Trash or Junk mailbox; use trash_message for intentional deletion")
		}
		if !selectableMailbox(mailboxes, input.Destination) {
			return nil, newError(CodeNotFound, "destination mailbox was not returned as selectable by LIST")
		}
		if err := selectMoveSource(session, input.Mailbox, input.UIDValidity); err != nil {
			return nil, err
		}
		for _, uid := range input.UIDs {
			if _, err := session.FetchFlags(uid); err != nil {
				return nil, err
			}
		}
		result := MoveBatchResult{
			Mailbox: input.Mailbox, UIDValidity: input.UIDValidity, Destination: input.Destination,
			Moved: make([]MovedMessage, 0, len(input.UIDs)),
		}
		for index, uid := range input.UIDs {
			moved, err := moveSelectedUID(ctx, session, phase, uid, input.Destination)
			if err != nil {
				if len(result.Moved) == 0 {
					return nil, err
				}
				public := AsError(mapIMAPError(err, ctx, true, reconciliation))
				failure := &MoveBatchFailure{UID: uid, Code: CodeOutcomeUnknown, Message: "mail move outcome is unknown", Reconciliation: reconciliation}
				if public != nil {
					failure.Code, failure.Message = public.Code, public.Message
					if public.Reconciliation != "" {
						failure.Reconciliation = public.Reconciliation
					}
				}
				result.Failed = failure
				result.NotAttempted = append([]uint32(nil), input.UIDs[index+1:]...)
				return result, nil
			}
			result.Moved = append(result.Moved, moved)
		}
		result.Complete = true
		return result, nil
	})
	if err != nil {
		return MoveBatchResult{}, err
	}
	return value.(MoveBatchResult), nil
}

func validateMoveBatch(input MoveBatchInput) error {
	if len(input.UIDs) == 0 || len(input.UIDs) > MaxMoveBatch {
		return validationError("uids must contain 1 to 50 message UIDs")
	}
	seen := make(map[uint32]struct{}, len(input.UIDs))
	for _, uid := range input.UIDs {
		if err := validateIdentity(input.Mailbox, input.UIDValidity, uid); err != nil {
			return err
		}
		if _, duplicate := seen[uid]; duplicate {
			return validationError("uids must be unique")
		}
		seen[uid] = struct{}{}
	}
	if err := validateMailbox(input.Destination); err != nil {
		return err
	}
	if input.Destination == input.Mailbox {
		return validationError("destination must differ from the source mailbox")
	}
	return nil
}
