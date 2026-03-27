// Package mapper converts supported ATProto records into SSB-compatible payloads.
package mapper

import (
	"encoding/json"
	"fmt"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

// RecordType* constants identify supported ATProto collections.
const (
	RecordTypePost    = "app.bsky.feed.post"
	RecordTypeLike    = "app.bsky.feed.like"
	RecordTypeRepost  = "app.bsky.feed.repost"
	RecordTypeFollow  = "app.bsky.graph.follow"
	RecordTypeBlock   = "app.bsky.graph.block"
	RecordTypeProfile = "app.bsky.actor.profile"
)

// MapRecord maps one ATProto record payload into an SSB message map.
func MapRecord(recordType string, rawJSON []byte) (map[string]interface{}, error) {
	switch recordType {
	case RecordTypePost:
		return mapPost(rawJSON)
	case RecordTypeLike:
		return mapLike(rawJSON)
	case RecordTypeRepost:
		return mapRepost(rawJSON)
	case RecordTypeFollow:
		return mapFollow(rawJSON)
	case RecordTypeBlock:
		return mapBlock(rawJSON)
	case RecordTypeProfile:
		return mapProfile(rawJSON)
	default:
		return nil, fmt.Errorf("unsupported record type: %s", recordType)
	}
}

func mapPost(rawJSON []byte) (map[string]interface{}, error) {
	var post appbsky.FeedPost
	if err := json.Unmarshal(rawJSON, &post); err != nil {
		return nil, err
	}

	res := map[string]interface{}{
		"type": "post",
		"text": post.Text,
	}

	// Preserve mention metadata so it can be resolved to SSB feed refs later.
	if len(post.Facets) > 0 {
		var mentions []map[string]string
		for _, facet := range post.Facets {
			for _, feat := range facet.Features {
				if feat.RichtextFacet_Mention != nil {
					mentions = append(mentions, map[string]string{
						"link": feat.RichtextFacet_Mention.Did,
						"name": "atproto_user", // Placeholder until DID->feed resolution is available.
					})
				}
			}
		}
		if len(mentions) > 0 {
			res["mentions"] = mentions
		}
	}

	// Preserve reply references as AT URIs for a later resolution pass.
	if post.Reply != nil {
		res["_atproto_reply_root"] = post.Reply.Root.Uri
		res["_atproto_reply_parent"] = post.Reply.Parent.Uri
	}

	return res, nil
}

func mapLike(rawJSON []byte) (map[string]interface{}, error) {
	var like appbsky.FeedLike
	if err := json.Unmarshal(rawJSON, &like); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"type":             "vote",
		"vote":             map[string]interface{}{"value": 1, "expression": "Like"},
		"_atproto_subject": like.Subject.Uri,
	}, nil
}

func mapRepost(rawJSON []byte) (map[string]interface{}, error) {
	var repost appbsky.FeedRepost
	if err := json.Unmarshal(rawJSON, &repost); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"type":                    "post",
		"text":                    "", // Reposts typically contain no text unless they are quote-post variants.
		"_atproto_repost_subject": repost.Subject.Uri,
	}, nil
}

func mapFollow(rawJSON []byte) (map[string]interface{}, error) {
	var follow appbsky.GraphFollow
	if err := json.Unmarshal(rawJSON, &follow); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"type":             "contact",
		"following":        true,
		"_atproto_contact": follow.Subject,
	}, nil
}

func mapBlock(rawJSON []byte) (map[string]interface{}, error) {
	var block appbsky.GraphBlock
	if err := json.Unmarshal(rawJSON, &block); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"type":             "contact",
		"blocking":         true,
		"_atproto_contact": block.Subject,
	}, nil
}

func mapProfile(rawJSON []byte) (map[string]interface{}, error) {
	var profile appbsky.ActorProfile
	if err := json.Unmarshal(rawJSON, &profile); err != nil {
		return nil, err
	}

	res := map[string]interface{}{
		"type": "about",
	}

	if profile.DisplayName != nil {
		res["name"] = *profile.DisplayName
	}

	if profile.Description != nil {
		res["description"] = *profile.Description
	}

	if profile.Avatar != nil {
		// Keep the CID so blob bridging can fetch and replace it with an SSB blob ref.
		res["_atproto_avatar_cid"] = profile.Avatar.Ref.String()
	}

	return res, nil
}

// ReplaceATProtoRefs resolves ATProto URI and DID placeholders in msg to SSB refs.
func ReplaceATProtoRefs(msg map[string]interface{}, lookupURI func(string) string, lookupDID func(string) string) {
	if rootURI, ok := msg["_atproto_reply_root"].(string); ok {
		if ssbRef := lookupURI(rootURI); ssbRef != "" {
			msg["root"] = ssbRef
		}
		delete(msg, "_atproto_reply_root")
	}

	if parentURI, ok := msg["_atproto_reply_parent"].(string); ok {
		if ssbRef := lookupURI(parentURI); ssbRef != "" {
			msg["branch"] = ssbRef
		}
		delete(msg, "_atproto_reply_parent")
	}

	if subjURI, ok := msg["_atproto_subject"].(string); ok {
		if ssbRef := lookupURI(subjURI); ssbRef != "" {
			if v, isMap := msg["vote"].(map[string]interface{}); isMap {
				v["link"] = ssbRef
			}
		}
		delete(msg, "_atproto_subject")
	}

	if repostURI, ok := msg["_atproto_repost_subject"].(string); ok {
		if ssbRef := lookupURI(repostURI); ssbRef != "" {
			// Many SSB clients recognize repost intent when the referenced message ID appears in text.
			msg["text"] = fmt.Sprintf("[%s]", ssbRef)
		}
		delete(msg, "_atproto_repost_subject")
	}

	if contactDID, ok := msg["_atproto_contact"].(string); ok {
		if ssbFeed := lookupDID(contactDID); ssbFeed != "" {
			msg["contact"] = ssbFeed
		}
		delete(msg, "_atproto_contact")
	}

	// Resolve mention links that still point at DIDs.
	if mentions, ok := msg["mentions"].([]map[string]string); ok {
		for i, m := range mentions {
			if did, hasLink := m["link"]; hasLink && strings.HasPrefix(did, "did:") {
				if ssbFeed := lookupDID(did); ssbFeed != "" {
					mentions[i]["link"] = ssbFeed
				}
			}
		}
	}
}
