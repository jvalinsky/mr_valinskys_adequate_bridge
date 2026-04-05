// Package mapper converts supported ATProto records into SSB-compatible payloads.
package mapper

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
)

// RecordType* constants identify supported ATProto collections.
const (
	RecordTypePost       = "app.bsky.feed.post"
	RecordTypeLike       = "app.bsky.feed.like"
	RecordTypeRepost     = "app.bsky.feed.repost"
	RecordTypeFollow     = "app.bsky.graph.follow"
	RecordTypeBlock      = "app.bsky.graph.block"
	RecordTypeProfile    = "app.bsky.actor.profile"
	RecordTypeList       = "app.bsky.graph.list"
	RecordTypeListItem   = "app.bsky.graph.listitem"
	RecordTypeThreadgate = "app.bsky.feed.threadgate"
)

// MapRecord maps one ATProto record payload into an SSB message map.
func MapRecord(recordType, atDID string, rawJSON []byte) (map[string]interface{}, error) {
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
		return mapProfile(atDID, rawJSON)
	case RecordTypeList:
		return mapList(rawJSON)
	case RecordTypeListItem:
		return mapListItem(rawJSON)
	case RecordTypeThreadgate:
		return mapThreadgate(rawJSON)
	default:
		return nil, fmt.Errorf("unsupported record type: %s", recordType)
	}
}

func mapPost(rawJSON []byte) (map[string]interface{}, error) {
	var post appbsky.FeedPost
	if err := json.Unmarshal(rawJSON, &post); err != nil {
		return nil, err
	}

	text, mentions := rewriteRichText(post.Text, post.Facets, post.Tags)
	res := map[string]interface{}{
		"type": "post",
		"text": text,
	}
	if len(mentions) > 0 {
		res["mentions"] = mentions
	}

	if post.Reply != nil {
		if post.Reply.Root != nil {
			res["_atproto_reply_root"] = post.Reply.Root.Uri
		}
		if post.Reply.Parent != nil {
			res["_atproto_reply_parent"] = post.Reply.Parent.Uri
		}
	}

	if quoteURI := quoteSubjectURI(post.Embed); quoteURI != "" {
		res["_atproto_quote_subject"] = quoteURI
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
		"text":                    "",
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
		"blocking":         false,
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
		"following":        false,
		"blocking":         true,
		"_atproto_contact": block.Subject,
	}, nil
}

func mapProfile(atDID string, rawJSON []byte) (map[string]interface{}, error) {
	var profile appbsky.ActorProfile
	if err := json.Unmarshal(rawJSON, &profile); err != nil {
		return nil, err
	}

	res := map[string]interface{}{
		"type":               "about",
		"_atproto_about_did": atDID,
	}

	if profile.DisplayName != nil {
		res["name"] = *profile.DisplayName
	}

	if profile.Description != nil {
		res["description"] = *profile.Description
	}

	return res, nil
}

type listRecord struct {
	Name        string `json:"name"`
	Purpose     string `json:"purpose"`
	Description string `json:"description"`
}

func mapList(rawJSON []byte) (map[string]interface{}, error) {
	var list listRecord
	if err := json.Unmarshal(rawJSON, &list); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"type":        "list",
		"name":        list.Name,
		"purpose":     list.Purpose,
		"description": list.Description,
	}, nil
}

type listItemRecord struct {
	List    string `json:"list"`
	Subject string `json:"subject"`
}

func mapListItem(rawJSON []byte) (map[string]interface{}, error) {
	var item listItemRecord
	if err := json.Unmarshal(rawJSON, &item); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"type":             "listitem",
		"_atproto_list":    item.List,
		"_atproto_contact": item.Subject,
	}, nil
}

type threadgateRecord struct {
	Post  string        `json:"post"`
	Allow []interface{} `json:"allow"`
}

func mapThreadgate(rawJSON []byte) (map[string]interface{}, error) {
	var gate threadgateRecord
	if err := json.Unmarshal(rawJSON, &gate); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"type":             "threadgate",
		"_atproto_subject": gate.Post,
		"allow":            gate.Allow,
	}, nil
}


type textFacet struct {
	start int
	end   int
	facet *appbsky.RichtextFacet
}

func rewriteRichText(text string, facets []*appbsky.RichtextFacet, tags []string) (string, []map[string]interface{}) {
	textBytes := []byte(text)
	ranged := make([]textFacet, 0, len(facets))
	for _, facet := range facets {
		if facet == nil || facet.Index == nil {
			continue
		}
		ranged = append(ranged, textFacet{
			start: int(facet.Index.ByteStart),
			end:   int(facet.Index.ByteEnd),
			facet: facet,
		})
	}
	sort.Slice(ranged, func(i, j int) bool {
		if ranged[i].start == ranged[j].start {
			return ranged[i].end < ranged[j].end
		}
		return ranged[i].start < ranged[j].start
	})

	var builder strings.Builder
	mentions := make([]map[string]interface{}, 0, len(facets)+len(tags))
	seenMentions := make(map[string]struct{})
	prev := 0
	for _, current := range ranged {
		if current.start < prev || current.start < 0 || current.end < current.start || current.end > len(textBytes) {
			continue
		}
		builder.Write(textBytes[prev:current.start])

		segment := string(textBytes[current.start:current.end])
		builder.WriteString(rewriteFacetSegment(segment, current.facet))
		mentions = appendFacetMentions(mentions, seenMentions, segment, current.facet)
		prev = current.end
	}
	builder.Write(textBytes[prev:])

	for _, tag := range tags {
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "#"))
		if tag == "" {
			continue
		}
		mentions = appendUniqueMention(mentions, seenMentions, map[string]interface{}{
			"link": "#" + tag,
			"name": "#" + tag,
		})
	}

	return builder.String(), mentions
}

func rewriteFacetSegment(segment string, facet *appbsky.RichtextFacet) string {
	if facet == nil {
		return segment
	}
	for _, feature := range facet.Features {
		if feature == nil || feature.RichtextFacet_Link == nil {
			continue
		}
		uri := strings.TrimSpace(feature.RichtextFacet_Link.Uri)
		if uri == "" || strings.TrimSpace(segment) == uri {
			return segment
		}
		return fmt.Sprintf("[%s](%s)", segment, uri)
	}
	return segment
}

func appendFacetMentions(mentions []map[string]interface{}, seen map[string]struct{}, segment string, facet *appbsky.RichtextFacet) []map[string]interface{} {
	if facet == nil {
		return mentions
	}
	for _, feature := range facet.Features {
		if feature == nil {
			continue
		}
		switch {
		case feature.RichtextFacet_Mention != nil:
			mentions = appendUniqueMention(mentions, seen, map[string]interface{}{
				"link": feature.RichtextFacet_Mention.Did,
				"name": segment,
			})
		case feature.RichtextFacet_Tag != nil:
			tag := strings.TrimSpace(strings.TrimPrefix(feature.RichtextFacet_Tag.Tag, "#"))
			if tag == "" {
				continue
			}
			name := segment
			if strings.TrimSpace(name) == "" {
				name = "#" + tag
			}
			mentions = appendUniqueMention(mentions, seen, map[string]interface{}{
				"link": "#" + tag,
				"name": name,
			})
		}
	}
	return mentions
}

func quoteSubjectURI(embed *appbsky.FeedPost_Embed) string {
	if embed == nil {
		return ""
	}
	if embed.EmbedRecord != nil && embed.EmbedRecord.Record != nil {
		return strings.TrimSpace(embed.EmbedRecord.Record.Uri)
	}
	if embed.EmbedRecordWithMedia != nil &&
		embed.EmbedRecordWithMedia.Record != nil &&
		embed.EmbedRecordWithMedia.Record.Record != nil {
		return strings.TrimSpace(embed.EmbedRecordWithMedia.Record.Record.Uri)
	}
	return ""
}

// ReplaceATProtoRefs resolves ATProto URI and DID placeholders in msg to SSB refs.
func ReplaceATProtoRefs(msg map[string]interface{}, lookupURI func(string) string, lookupDID func(string) string) {
	if rootURI, ok := msg["_atproto_reply_root"].(string); ok {
		if ssbRef := lookupURI(rootURI); ssbRef != "" {
			msg["root"] = ssbRef
			delete(msg, "_atproto_reply_root")
		}
	}

	if listURI, ok := msg["_atproto_list"].(string); ok {
		if ssbRef := lookupURI(listURI); ssbRef != "" {
			msg["list"] = ssbRef
			delete(msg, "_atproto_list")
		}
	}

	if parentURI, ok := msg["_atproto_reply_parent"].(string); ok {
		if ssbRef := lookupURI(parentURI); ssbRef != "" {
			msg["branch"] = ssbRef
			delete(msg, "_atproto_reply_parent")
		}
	}

	if subjURI, ok := msg["_atproto_subject"].(string); ok {
		if ssbRef := lookupURI(subjURI); ssbRef != "" {
			if v, isMap := msg["vote"].(map[string]interface{}); isMap {
				v["link"] = ssbRef
			}
			delete(msg, "_atproto_subject")
		}
	}

	if quoteURI, ok := msg["_atproto_quote_subject"].(string); ok {
		if ssbRef := lookupURI(quoteURI); ssbRef != "" {
			mentions := normalizedMentions(msg["mentions"])
			mentions = appendUniqueMention(mentions, make(map[string]struct{}), map[string]interface{}{
				"link": ssbRef,
				"name": "quoted post",
			})
			msg["mentions"] = mentions
			msg["text"] = appendMarkdownBlock(asString(msg["text"]), fmt.Sprintf("[quoted post](%s)", ssbRef))
			delete(msg, "_atproto_quote_subject")
		}
	}

	if contactDID, ok := msg["_atproto_contact"].(string); ok {
		if ssbFeed := lookupDID(contactDID); ssbFeed != "" {
			msg["contact"] = ssbFeed
			delete(msg, "_atproto_contact")
		}
	}

	if aboutDID, ok := msg["_atproto_about_did"].(string); ok {
		if ssbFeed := lookupDID(aboutDID); ssbFeed != "" {
			msg["about"] = ssbFeed
			delete(msg, "_atproto_about_did")
		}
	}

	mentions := normalizedMentions(msg["mentions"])
	resolved := make([]map[string]interface{}, 0, len(mentions))
	for _, mention := range mentions {
		link, _ := mention["link"].(string)
		if !strings.HasPrefix(link, "did:") {
			resolved = append(resolved, mention)
			continue
		}
		if ssbFeed := lookupDID(link); ssbFeed != "" {
			mention["link"] = ssbFeed
			resolved = append(resolved, mention)
		}
	}
	if len(resolved) > 0 {
		msg["mentions"] = resolved
	} else {
		delete(msg, "mentions")
	}
}

// SanitizeForPublish removes internal _atproto_* bookkeeping fields from a mapped
// message before it is published to the SSB log. These fields are bridge-internal
// and must never appear in published messages — Planetary's strict Codable decoders
// can crash when encountering unexpected fields during batch FFI processing.
func SanitizeForPublish(msg map[string]interface{}) {
	for key := range msg {
		if strings.HasPrefix(key, "_atproto_") {
			delete(msg, key)
		}
	}
}

// ReadyForPublish returns true if the mapped message has all required fields for
// its SSB content type. Contact messages need a non-empty "contact" field; vote
// messages need a "link" inside the "vote" object.
func ReadyForPublish(msg map[string]interface{}) bool {
	msgType, _ := msg["type"].(string)
	switch msgType {
	case "contact":
		contact, _ := msg["contact"].(string)
		return strings.TrimSpace(contact) != ""
	case "vote":
		vote, ok := msg["vote"].(map[string]interface{})
		if !ok {
			return false
		}
		link, _ := vote["link"].(string)
		return strings.TrimSpace(link) != ""
	default:
		return true
	}
}

// UnresolvedATProtoRefs reports unresolved reference placeholders after replacement.
func UnresolvedATProtoRefs(msg map[string]interface{}) []string {
	keys := []string{
		"_atproto_reply_root",
		"_atproto_reply_parent",
		"_atproto_subject",
		"_atproto_quote_subject",
		"_atproto_contact",
		"_atproto_list",
		"_atproto_about_did",
	}

	unresolved := make([]string, 0, len(keys))
	for _, key := range keys {
		if raw, ok := msg[key]; ok && strings.TrimSpace(fmt.Sprint(raw)) != "" {
			unresolved = append(unresolved, fmt.Sprintf("%s=%v", key, raw))
		}
	}
	return unresolved
}

func appendUniqueMention(mentions []map[string]interface{}, seen map[string]struct{}, mention map[string]interface{}) []map[string]interface{} {
	link := strings.TrimSpace(asString(mention["link"]))
	if link == "" {
		return mentions
	}
	key := link + "|" + strings.TrimSpace(asString(mention["name"]))
	if _, ok := seen[key]; ok {
		return mentions
	}
	seen[key] = struct{}{}
	mentions = append(mentions, mention)
	return mentions
}

func normalizedMentions(raw interface{}) []map[string]interface{} {
	switch typed := raw.(type) {
	case nil:
		return nil
	case []map[string]interface{}:
		return typed
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			m, ok := item.(map[string]interface{})
			if ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func appendMarkdownBlock(text, block string) string {
	text = strings.TrimRight(text, "\n")
	block = strings.TrimSpace(block)
	if block == "" {
		return text
	}
	if text == "" {
		return block
	}
	return text + "\n\n" + block
}

func asString(raw interface{}) string {
	if raw == nil {
		return ""
	}
	return fmt.Sprint(raw)
}
