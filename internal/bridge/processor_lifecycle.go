package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
)

type mappedLifecycleOutcome struct {
	State        string
	PublishErr   error
	PublishedRef string
}

func (p *Processor) applyMappedMessageLifecycle(ctx context.Context, msg *db.Message, mapped map[string]interface{}, blobErr error, sanitize bool) mappedLifecycleOutcome {
	unresolved := mapper.UnresolvedATProtoRefs(mapped)
	if len(unresolved) > 0 {
		now := time.Now().UTC()
		p.markDeferred(msg, strings.Join(unresolved, ";"), now)
		if blobErr != nil {
			msg.PublishError = annotateBlobFallback(msg.PublishError, blobErr)
		}
		return mappedLifecycleOutcome{State: db.MessageStateDeferred}
	}

	if p.publisher == nil {
		msg.MessageState = db.MessageStatePending
		if blobErr != nil {
			msg.PublishError = annotateBlobFallback(msg.PublishError, blobErr)
		}
		return mappedLifecycleOutcome{State: db.MessageStatePending}
	}

	if sanitize {
		mapper.SanitizeForPublish(mapped)
		if !mapper.ReadyForPublish(mapped) {
			now := time.Now().UTC()
			p.markDeferred(msg, deferReasonMissingRequiredFieldsAfterSanitize, now)
			return mappedLifecycleOutcome{State: db.MessageStateDeferred}
		}
		rawSSBJSON, marshalErr := json.Marshal(mapped)
		if marshalErr == nil {
			msg.RawSSBJson = string(rawSSBJSON)
		}
	}

	attemptedAt := time.Now().UTC()
	p.markPublishAttempt(msg, attemptedAt)
	msg.DeferReason = ""

	ssbMsgRef, publishErr := p.publisher.Publish(ctx, msg.ATDID, mapped)
	if publishErr != nil {
		p.markPublishFailed(msg, publishErr)
		if blobErr != nil {
			msg.PublishError = annotateBlobFallback(msg.PublishError, blobErr)
		}
		return mappedLifecycleOutcome{
			State:      db.MessageStateFailed,
			PublishErr: publishErr,
		}
	}

	publishedAt := time.Now().UTC()
	p.markPublished(msg, ssbMsgRef, publishedAt)
	if blobErr != nil {
		msg.PublishError = annotateBlobFallback("", blobErr)
	}
	return mappedLifecycleOutcome{
		State:        db.MessageStatePublished,
		PublishedRef: ssbMsgRef,
	}
}
