package mapper

import (
	"encoding/json"
	"fmt"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

// RecordType constants
const (
	RecordTypePost    = "app.bsky.feed.post"
	RecordTypeLike    = "app.bsky.feed.like"
	RecordTypeRepost  = "app.bsky.feed.repost"
	RecordTypeFollow  = "app.bsky.graph.follow"
	RecordTypeBlock   = "app.bsky.graph.block"
	RecordTypeProfile = "app.bsky.actor.profile"
)

// MapRecord takes a raw ATProto record JSON and its type, returning an SSB message map
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

	// Basic mentions mapping
	// In SSB, mentions are typically included in the text as @name
	// and there's a "mentions" array of feed refs
	if len(post.Facets) > 0 {
		var mentions []map[string]string
		for _, facet := range post.Facets {
			for _, feat := range facet.Features {
				if feat.RichtextFacet_Mention != nil {
					// We'd ideally need a way to lookup the DID to SSB Feed ID here.
					// For now, we'll store the DID in a generic way, or just an AT URI.
					mentions = append(mentions, map[string]string{
						"link": feat.RichtextFacet_Mention.Did,
						"name": "atproto_user", // Placeholder
					})
				}
			}
		}
		if len(mentions) > 0 {
			res["mentions"] = mentions
		}
	}

	// Handle reply refs
	if post.Reply != nil {
		// In SSB, replies have "root" and "branch"
		// Root is the first message in thread, branch is the message being replied to
		// Here we'd need to resolve AT URIs to SSB msg refs.
		// We'll store AT URIs for the bridge to resolve later before publishing, or store as AT URIs.
		// For a pure mapper, let's include the AT URIs and expect a post-processing step to replace them.
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
		"text":                    "", // Retweets usually don't have text in AT unless quote post (not standard repost)
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
		// Would need to fetch and store blob, returning blob ref
		res["_atproto_avatar_cid"] = profile.Avatar.Ref.String()
	}

	return res, nil
}

// ReplaceATProtoRefs is a post-processing function to replace ATProto URIs/DIDs with SSB refs
// using a provided lookup function.
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
			// standard SSB retweet approach uses a 'repost' or 'share' link in 'text' or 'branch'
			// or sometimes just mentioning the root. In many SSB clients, mentioning the message ID works.
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

	// Mentions
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
